package cgroup

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mitchellh/go-ps"
	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/log"

	"kubevirt.io/kubevirt/pkg/virt-controller/services"
)

func getDedicatedCpuCgroupManager(vmi *v1.VirtualMachineInstance) (Manager, error) {
	// Find dedicated cgroup slice and make a manager for it
	dedicatedCgroupSleepTime := services.CalculateBigUniqueValueForVmi(vmi)

	log.Log.Infof("ihol3 dedicatedCgroupSleepTime: %d", dedicatedCgroupSleepTime)

	// TODO ihol3
	procs, err := ps.Processes()
	if err != nil {
		log.Log.Infof("ihol3 error getting processes: %v", err)
		return nil, err
	}

	var sleepProcPid int

	for _, proc := range procs {
		//log.Log.Infof("ihol3 looking at proc: %s", proc.Executable())
		cmdline, err := os.ReadFile(filepath.Join("/", "proc", strconv.Itoa(proc.Pid()), "cmdline"))
		if err != nil {
			log.Log.Infof("ihol3 error reading cmdline: %v", err)
			return nil, err
		}

		//log.Log.Infof("ihol3 cmdline: %s", cmdline)

		if strings.Contains(string(cmdline), fmt.Sprintf("%d", dedicatedCgroupSleepTime)) {
			sleepProcPid = proc.Pid()
			log.Log.Infof("ihol3 YAY! pid found: %d!", proc.Pid())
			break
		}
	}

	dedicatedCpusCgroupManager, err := NewManagerFromPid(sleepProcPid)
	if err != nil {
		log.Log.Infof("ihol3 error cgroup manager: %v", err)
		return dedicatedCpusCgroupManager, err
	}

	return dedicatedCpusCgroupManager, nil
}

func getQemuKvmPid(computeCgroupManager Manager) (int, error) {
	// TODO: ihol3 document about expecting one qemu process
	// TODO: ihol3 clean errors and logs
	qemuKvmFilter := func(s string) bool { return strings.Contains(s, "qemu-kvm") }
	qemuKvmPids, err := computeCgroupManager.GetCgroupThreadsWithFilter(qemuKvmFilter)
	if err != nil {
		log.Log.Infof("ihol3 kvm filter err: %v", err)
		return -1, err
	} else if len(qemuKvmPids) == 0 {
		err := fmt.Errorf("qemu process was not found")
		log.Log.Infof("ihol3 %v", err)
		return -1, err
	}
	//else if len(qemuKvmPids) > 1 {
	//	err := fmt.Errorf("more than 1 qemu process is found within the compute container")
	//	log.Log.Infof("ihol3 %v", err)
	//	return -1, err
	//}

	return qemuKvmPids[0], nil
}

func getVcpuTids(computeCgroupManager Manager) ([]int, error) {
	vcpusFilter := func(s string) bool { return strings.Contains(s, "CPU ") && strings.Contains(s, "KVM") }
	vcpuTids, err := computeCgroupManager.GetCgroupThreadsWithFilter(vcpusFilter)
	if err != nil {
		log.Log.Infof("ihol3 vcpus filter err: %v", vcpuTids)
		return nil, err
	}

	return vcpuTids, nil
}

func dedicatedCpuHelper(computeCgroupManager Manager, vmi *v1.VirtualMachineInstance) (dedicatedCpuManager Manager, qemuKvmPid int, vcpuTids []int, err error) {
	if !vmi.IsCPUDedicated() {
		err = fmt.Errorf("vmi %s is expected to be defined with dedicated CPUs", vmi.Name)
		return
	}

	dedicatedCpusCgroupManager, err := getDedicatedCpuCgroupManager(vmi)
	if err != nil {
		return
	}

	qemuKvmPid, err = getQemuKvmPid(computeCgroupManager)
	if err != nil {
		return
	}
	log.Log.Infof("ihol3 qemu-kvm pid: %+v", qemuKvmPid)

	err = dedicatedCpusCgroupManager.AttachTask(qemuKvmPid, "", Process)
	if err != nil {
		log.Log.Infof("ihol3 attach qemu: %v", err)
		return
	}

	housekeepingCgroupManager, err := dedicatedCpusCgroupManager.CreateChildCgroup("housekeeping", CgroupSubsystemCpuset)
	if err != nil {
		log.Log.Infof("ihol3 create hk cgroup: %v", err)
		return
	}

	err = housekeepingCgroupManager.MakeThreaded()
	if err != nil {
		log.Log.Infof("ihol3 error making cgroup threaded: %v", err)
		return
	}

	vcpuTids, err = getVcpuTids(computeCgroupManager)
	if err != nil {
		return
	}

	return
}
