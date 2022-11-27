package cgroup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/log"

	runc_cgroups "github.com/opencontainers/runc/libcontainer/cgroups"
	runc_fs "github.com/opencontainers/runc/libcontainer/cgroups/fs2"
	runc_configs "github.com/opencontainers/runc/libcontainer/configs"
	"github.com/opencontainers/runc/libcontainer/devices"

	"kubevirt.io/kubevirt/pkg/util"
)

var rulesPerPid = make(map[string][]*devices.Rule)

type cgroupV2File string

const (
	subtreeControl cgroupV2File = "cgroup.subtree_control"
	cgroupType     cgroupV2File = "cgroup.type"
)

type cgroupV2Type string

const (
	domain         cgroupV2Type = "domain"
	threaded       cgroupV2Type = "threaded"
	domainThreaded cgroupV2Type = "domainThreaded"
	domainInvalid  cgroupV2Type = "domainInvalid"
)

type cgroupV2SubtreeCtrlAction string

const (
	subtreeCtrlAdd    cgroupV2SubtreeCtrlAction = "+"
	subtreeCtrlRemove cgroupV2SubtreeCtrlAction = "-"
)

type v2Manager struct {
	runc_cgroups.Manager
	dirPath        string
	isRootless     bool
	execVirtChroot execVirtChrootFunc
}

func newV2Manager(dirPath string) (Manager, error) {
	config := getDeafulCgroupConfig()

	runcManager, err := runc_fs.NewManager(config, dirPath)
	if err != nil {
		return nil, err
	}

	return newCustomizedV2Manager(runcManager, config.Rootless, execVirtChrootCgroups)
}

func newCustomizedV2Manager(runcManager runc_cgroups.Manager, isRootless bool, execVirtChroot execVirtChrootFunc) (Manager, error) {
	manager := v2Manager{
		runcManager,
		runcManager.GetPaths()[""],
		isRootless,
		execVirtChroot,
	}

	return &manager, nil
}

func (v *v2Manager) GetBasePathToHostSubsystem(_ string) (string, error) {
	return v.dirPath, nil
}

func (v *v2Manager) Set(r *runc_configs.Resources) error {
	// We want to keep given resources untouched
	resourcesToSet := *r

	//Add default rules
	resourcesToSet.Devices = append(resourcesToSet.Devices, GenerateDefaultDeviceRules()...)

	rulesToSet, err := addCurrentRules(rulesPerPid[v.dirPath], resourcesToSet.Devices)
	if err != nil {
		return err
	}
	rulesPerPid[v.dirPath] = rulesToSet
	resourcesToSet.Devices = rulesToSet

	err = v.execVirtChroot(&resourcesToSet, map[string]string{"": v.dirPath}, v.isRootless, v.GetCgroupVersion())
	return err
}

func (v *v2Manager) GetCgroupVersion() CgroupVersion {
	return V2
}

func (v *v2Manager) GetCpuSet() ([]int, error) {
	return getCpuSetPath(v, "cpuset.cpus.effective")
}

func (v *v2Manager) mutateSubtreeControl(subSystems string, action cgroupV2SubtreeCtrlAction) error {
	return runc_cgroups.WriteFile(v.dirPath, string(subtreeControl), fmt.Sprintf("%s%s", string(action), subSystems))
}

func (v *v2Manager) CreateChildCgroup(name string, subSystems ...string) (Manager, error) {
	newGroupPath := filepath.Join(v.dirPath, name)
	if _, err := os.Stat(newGroupPath); !errors.Is(err, os.ErrNotExist) {
		return NewManagerFromPath(map[string]string{"": newGroupPath})
	}

	log := func(s string) {
		log.Log.Infof("ihol3 CreateChildCgroup() %s", s)
	}

	readFromFile := func(dir, file string) string {
		fileContent, err := runc_cgroups.ReadFile(dir, file)
		if err != nil {
			log("readFromFile() ERR: " + err.Error())
		}

		return fileContent
	}

	log("newGroupPath: " + newGroupPath)

	// TODO: ihol3 subsystem and controller terminology is mxied up here
	// Remove unnecessary controllers from subtree control. This is crucial in order to make the cgroup threaded
	curSubtreeControllers := readFromFile(v.dirPath, string(subtreeControl))
	for _, curSubtreeController := range strings.Split(curSubtreeControllers, " ") {
		if curSubtreeController == "" {
			continue
		}

		for _, subSystem := range subSystems {
			if curSubtreeController == subSystem {
				continue
			}
		}

		log("trying to remove controller " + curSubtreeController + " from subtree control")
		err := v.mutateSubtreeControl(curSubtreeController, subtreeCtrlRemove)
		if err != nil {
			return nil, err
		}
	}

	// Configure the given subsystems to be inherited by the new cgroup
	for _, subSystem := range subSystems {
		err := v.mutateSubtreeControl(subSystem, subtreeCtrlAdd)
		if err != nil {
			return nil, err
		}
	}

	log("parent subtree control: " + readFromFile(v.dirPath, string(subtreeControl)))

	// Create a new cgroup directory
	err := util.MkdirAllWithNosec(newGroupPath)
	if err != nil {
		return nil, fmt.Errorf("failed creating cgroup directory %s: %v", newGroupPath, err)
	}

	log("parent controllers: " + readFromFile(v.dirPath, "cgroup.controllers"))
	log("child controllers: " + readFromFile(newGroupPath, "cgroup.controllers"))
	log("cgroup type: " + readFromFile(newGroupPath, "cgroup.type"))

	newManager, err := NewManagerFromPath(map[string]string{"": newGroupPath})
	if err != nil {
		return newManager, err
	}

	return newManager, nil
}

// Attach TID to cgroup. Optionally on a subcgroup of
// the pods control group (if subcgroup != nil).
func (v *v2Manager) AttachTask(id int, _ string, taskType TaskType) error {
	var targetFile string
	switch taskType {
	case Thread:
		targetFile = "cgroup.threads"
	case Process:
		targetFile = "cgroup.procs"
	default:
		return fmt.Errorf("task type %v is not valid", taskType)
	}

	err := runc_cgroups.WriteFile(v.dirPath, targetFile, strconv.Itoa(id))
	if err != nil {
		return err
	}

	return nil
}

func (v *v2Manager) GetCgroupThreadsWithFilter(filter func(string) bool) ([]int, error) {
	return getCgroupThreadsHelper(v, "cgroup.threads", filter)
}

func (v *v2Manager) GetCgroupThreads() ([]int, error) {
	return v.GetCgroupThreadsWithFilter(nil)
}

func (v *v2Manager) SetCpuSet(cpulist []int) error {
	return setCpuSetHelper(v, cpulist)
}

func (v *v2Manager) MakeThreaded() error {
	// TODO: ihol3 link ticket for runc?
	const (
		cgTypeFile   = "cgroup.type"
		typeThreaded = "threaded"
	)

	cgroupType, err := runc_cgroups.ReadFile(v.dirPath, cgTypeFile)
	log.Log.Infof("ihol3 MakeThreaded(): cgroup type before any changes: %s", cgroupType)
	if err != nil {
		return err
	}
	cgroupType = strings.TrimSpace(cgroupType)

	if cgroupType == typeThreaded {
		log.Log.Infof("ihol3 MakeThreaded(): cgroup already threaded")
		return nil
	}

	err = runc_cgroups.WriteFile(v.dirPath, cgTypeFile, typeThreaded)
	if err != nil {
		return err
	}

	cgroupType, err = runc_cgroups.ReadFile(v.dirPath, cgTypeFile)
	if err != nil {
		return err
	}
	cgroupType = strings.TrimSpace(cgroupType)

	if cgroupType != typeThreaded {
		return fmt.Errorf("could not change cgroup type (%s) to %s", cgroupType, typeThreaded)
	}

	return nil
}

func (v *v2Manager) HandleDedicatedCpus(vmi *v1.VirtualMachineInstance) error {
	dedicatedCpusCgroupManager, qemuKvmPid, vcpuTids, err := dedicatedCpuHelper(v, vmi)
	if err != nil {
		return err
	}

	err = dedicatedCpusCgroupManager.AttachTask(qemuKvmPid, "", Process)
	if err != nil {
		log.Log.Infof("ihol3 attach qemu: %v", err)
		return err
	}

	housekeepingCgroupManager, err := dedicatedCpusCgroupManager.CreateChildCgroup("housekeeping", CgroupSubsystemCpuset)
	if err != nil {
		log.Log.Infof("ihol3 create hk cgroup: %v", err)
		return err
	}

	err = housekeepingCgroupManager.MakeThreaded()
	if err != nil {
		log.Log.Infof("ihol3 error making cgroup threaded: %v", err)
		return err
	}

	for _, vcpuTid := range vcpuTids {
		err = housekeepingCgroupManager.AttachTask(vcpuTid, "", Thread)
		if err != nil {
			log.Log.Infof("ihol3 attach vcpus: %v", err)
			return err
		}
	}

	cpuset, err := dedicatedCpusCgroupManager.GetCpuSet()
	if err != nil {
		log.Log.Infof("ihol3 getcpuset: %v", err)
		return err
	}

	if len(cpuset) < 2 {
		err = fmt.Errorf("ihol3 cpuset is expected to be at least of length 2 (for 1 vCPU and 1 extra code): %v", err)
		log.Log.Infof("%v", err)
		return err
	}

	housekeepingCore := cpuset[len(cpuset)-1:]
	log.Log.Infof("ihol3 housekeeping core: %d", housekeepingCore[0])
	err = housekeepingCgroupManager.SetCpuSet(housekeepingCore)
	if err != nil {
		log.Log.Infof("ihol3 setcpuset: %v", err)
		return err
	}

	log.Log.Infof("ihol3 YAY all ok :D")

	return nil
}
