/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2025 The KubeVirt Authors.
 *
 */

package performance

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"kubevirt.io/kubevirt/tests/exec"
	"kubevirt.io/kubevirt/tests/framework/matcher"
	"kubevirt.io/kubevirt/tests/libpod"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	expect "github.com/google/goexpect"

	v1 "kubevirt.io/api/core/v1"

	"kubevirt.io/kubevirt/pkg/libvmi"
	"kubevirt.io/kubevirt/tests/console"
	"kubevirt.io/kubevirt/tests/decorators"
	"kubevirt.io/kubevirt/tests/libvmifact"
	"kubevirt.io/kubevirt/tests/libvmops"
)

var _ = Describe("[sig-compute] Stress", decorators.SigCompute, decorators.RequiresNodeWithCPUManager, func() {
	Context("VM CPU stress", func() {
		It("virt-launcher should not starve when the guest stresses CPU", func() {
			By("Creating a VirtualMachineInstance")
			vmi := libvmifact.NewFedora(
				libvmi.WithCPUCount(1, 1, 1),
				libvmi.WithDedicatedCPUPlacement(),
			)
			vmi = libvmops.RunVMIAndExpectLaunch(vmi, 240)

			By("Logging into the VMI")
			Expect(console.LoginToFedora(vmi)).To(Succeed())
			Eventually(matcher.ThisVMI(vmi), 2*time.Minute, 2*time.Second).Should(matcher.HaveConditionTrue(v1.VirtualMachineInstanceAgentConnected))

			By("Ensuring that the vcpu cgroup exists")
			launcherPod, err := libpod.GetPodByVirtualMachineInstance(vmi, vmi.Namespace)
			Expect(err).NotTo(HaveOccurred())

			podOutput, err := exec.ExecuteCommandOnPod(
				launcherPod,
				launcherPod.Spec.Containers[0].Name,
				[]string{"sh", "-c",
					`ls /sys/fs/cgroup`,
				},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(podOutput).To(ContainSubstring("vcpu"))

			By("Running CPU stress inside the VMI")
			stressVMICpu(vmi)

			By("Running a computational heavy task on virt-launcher")
			podOutput, err = exec.ExecuteCommandOnPod(
				launcherPod,
				launcherPod.Spec.Containers[0].Name,
				[]string{"sh", "-c",
					`(time for i in $(seq 1 2000); do echo -n "test message" | sha256sum > /dev/null; done ) 2>&1 | grep real | awk '{print $2}' | sed 's/m/:/' | awk -F: '{print ($1 * 60) + $2}'`,
				},
			)
			Expect(err).NotTo(HaveOccurred())

			//convert podOutput to seconds in a float64
			secondsSpent, err := strconv.ParseFloat(strings.TrimSpace(podOutput), 64)
			Expect(err).NotTo(HaveOccurred())

			// check if the time spent is less than 10 seconds
			Expect(secondsSpent).To(BeNumerically("<", 0.0), fmt.Sprintf("Time spent on virt-launcher is too high (above 10 seconds): %f seconds", secondsSpent))
		})
	})
})

func stressVMICpu(vmi *v1.VirtualMachineInstance) {
	By("Run a stress test to dirty some pages and slow down the migration")
	const stressCmd = "stress-ng --cpu 0 --cpu-method all --matrix 0 --matrix-size 1024 &\n"

	Expect(console.SafeExpectBatch(vmi, []expect.Batcher{
		&expect.BSnd{S: "\n"},
		&expect.BExp{R: console.PromptExpression},
		&expect.BSnd{S: stressCmd},
		&expect.BExp{R: console.PromptExpression},
	}, 15)).To(Succeed(), "should run a stress test")

	// give stress tool some time to trash more memory pages before returning control to next steps
	time.Sleep(15 * time.Second)
}
