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
 * Copyright 2017, 2018 Red Hat, Inc.
 *
 */

package tests_test

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/onsi/gomega/gstruct"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"

	virt_api "kubevirt.io/kubevirt/pkg/virt-api"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kubev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"kubevirt.io/kubevirt/tests/util"

	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"

	"kubevirt.io/kubevirt/tests"
	"kubevirt.io/kubevirt/tests/console"
	cd "kubevirt.io/kubevirt/tests/containerdisk"
	"kubevirt.io/kubevirt/tests/libvmi"
)

var _ = Describe("[rfe_id:609][sig-compute]VMIheadless", func() {

	var err error
	var virtClient kubecli.KubevirtClient
	var vmi *v1.VirtualMachineInstance

	BeforeEach(func() {
		virtClient, err = kubecli.GetKubevirtClient()
		util.PanicOnError(err)

		vmi = tests.NewRandomVMIWithEphemeralDisk(cd.ContainerDiskFor(cd.ContainerDiskAlpine))
	})

	Describe("[rfe_id:609]Creating a VirtualMachineInstance", func() {

		Context("with headless", func() {

			BeforeEach(func() {
				f := false
				vmi.Spec.Domain.Devices.AutoattachGraphicsDevice = &f
			})

			It("[test_id:707]should create headless vmi without any issue", func() {
				tests.RunVMIAndExpectLaunch(vmi, 30)
			})

			It("[test_id:714][posneg:positive]should not have vnc graphic device in xml", func() {
				tests.RunVMIAndExpectLaunch(vmi, 30)

				runningVMISpec, err := tests.GetRunningVMIDomainSpec(vmi)
				Expect(err).ToNot(HaveOccurred(), "should get vmi spec without problem")

				Expect(runningVMISpec.Devices.Graphics).To(BeEmpty(), "should not have any graphics devices present")
			})

			It("[test_id:737][posneg:positive]should match memory with overcommit enabled", func() {
				vmi.Spec.Domain.Resources = v1.ResourceRequirements{
					Requests: kubev1.ResourceList{
						kubev1.ResourceMemory: resource.MustParse("100M"),
					},
					OvercommitGuestOverhead: true,
				}
				vmi = tests.RunVMIAndExpectLaunch(vmi, 30)

				readyPod := libvmi.GetPodByVirtualMachineInstance(vmi, util.NamespaceTestDefault)
				computeContainer := tests.GetComputeContainerOfPod(readyPod)

				Expect(computeContainer.Resources.Requests.Memory().String()).To(Equal("100M"))
			})

			It("[test_id:2444][posneg:negative]should not match memory with overcommit disabled", func() {
				vmi.Spec.Domain.Resources = v1.ResourceRequirements{
					Requests: kubev1.ResourceList{
						kubev1.ResourceMemory: resource.MustParse("100M"),
					},
				}
				vmi = tests.RunVMIAndExpectLaunch(vmi, 30)

				readyPod := libvmi.GetPodByVirtualMachineInstance(vmi, util.NamespaceTestDefault)
				computeContainer := tests.GetComputeContainerOfPod(readyPod)

				Expect(computeContainer.Resources.Requests.Memory().String()).ToNot(Equal("100M"))
			})

			It("[test_id:713]should have more memory on pod when headless", func() {
				normalVmi := tests.NewRandomVMIWithEphemeralDisk(cd.ContainerDiskFor(cd.ContainerDiskAlpine))

				vmi = tests.RunVMIAndExpectLaunch(vmi, 30)
				normalVmi = tests.RunVMIAndExpectLaunch(normalVmi, 30)

				readyPod := libvmi.GetPodByVirtualMachineInstance(vmi, util.NamespaceTestDefault)
				computeContainer := tests.GetComputeContainerOfPod(readyPod)

				normalReadyPod := libvmi.GetPodByVirtualMachineInstance(normalVmi, util.NamespaceTestDefault)
				normalComputeContainer := tests.GetComputeContainerOfPod(normalReadyPod)

				memDiff := normalComputeContainer.Resources.Requests.Memory()
				memDiff.Sub(*computeContainer.Resources.Requests.Memory())

				Expect(memDiff.ScaledValue(resource.Mega) > 15).To(BeTrue(),
					fmt.Sprintf("memory difference between headless (%s) and normal (%s) is %dM, but should be roughly 16M",
						computeContainer.Resources.Requests.Memory(),
						normalComputeContainer.Resources.Requests.Memory(),
						memDiff.ScaledValue(resource.Mega)))
			})

			It("[test_id:738][posneg:negative]should not connect to VNC", func() {
				vmi = tests.RunVMIAndExpectLaunch(vmi, 30)

				_, err := virtClient.VirtualMachineInstance(vmi.ObjectMeta.Namespace).VNC(vmi.ObjectMeta.Name)

				Expect(err.Error()).To(Equal("No graphics devices are present."), "vnc should not connect on headless VM")
			})

			It("[test_id:709][posneg:positive]should connect to console", func() {
				vmi = tests.RunVMIAndExpectLaunch(vmi, 30)

				By("checking that console works")
				Expect(console.LoginToAlpine(vmi)).To(Succeed())
			})

		})

		Context("without headless", func() {

			It("[test_id:714][posneg:negative]should have one vnc graphic device in xml", func() {
				tests.RunVMIAndExpectLaunch(vmi, 30)

				runningVMISpec, err := tests.GetRunningVMIDomainSpec(vmi)
				Expect(err).ToNot(HaveOccurred(), "should get vmi spec without problem")

				vncCount := 0
				for _, gr := range runningVMISpec.Devices.Graphics {
					if strings.ToLower(gr.Type) == "vnc" {
						vncCount += 1
					}
				}
				Expect(vncCount).To(Equal(1), "should have exactly one VNC device")
			})

			It("should close connections after a while", func() {
				getHandlerConns := func() int {
					cmd := []string{"bash", "-c", fmt.Sprintf("ss -ntlap | grep %d | wc -l", virt_api.DefaultConsoleServerPort)}
					stdout, stderr, err := tests.ExecuteCommandOnNodeThroughVirtHandler(virtClient, vmi.Status.NodeName, cmd)
					Expect(err).ToNot(HaveOccurred())
					Expect(stderr).To(BeEmpty())

					stdout = strings.TrimSpace(stdout)
					stdout = strings.ReplaceAll(stdout, "\n", "")

					handlerCons, err := strconv.Atoi(stdout)
					Expect(err).ToNot(HaveOccurred())

					return handlerCons
				}

				By("Running the VMI")
				vmi = tests.NewRandomFedoraVMIWithGuestAgent()
				vmi = tests.RunVMIAndExpectLaunch(vmi, 30)

				By("VMI has the guest agent connected condition")
				Eventually(func() []v1.VirtualMachineInstanceCondition {
					vmi, err = virtClient.VirtualMachineInstance(util.NamespaceTestDefault).Get(vmi.Name, &v12.GetOptions{})
					Expect(err).ToNot(HaveOccurred(), "Should get VMI ")
					return vmi.Status.Conditions
				}, 30*time.Second, 2).Should(
					ContainElement(
						gstruct.MatchFields(
							gstruct.IgnoreExtras,
							gstruct.Fields{"Type": Equal(v1.VirtualMachineInstanceAgentConnected)})),
					"Should have agent connected condition")

				origHandlerCons := getHandlerConns()

				By("Making multiple requests")
				const numberOfRequests = 30
				for i := 0; i < numberOfRequests; i++ {
					_, err := virtClient.VirtualMachineInstance(vmi.ObjectMeta.Namespace).GuestOsInfo(vmi.Name)
					Expect(err).ToNot(HaveOccurred())
					time.Sleep(500 * time.Millisecond)
				}

				By("Expecting the number of connections to stabalize")
				Eventually(func() int {
					return getHandlerConns() - origHandlerCons
				}, 30*time.Second, 1*time.Second).Should(BeNumerically("<=", 5), "number of connections is expected to drop after a while")
			})

		})

	})

})
