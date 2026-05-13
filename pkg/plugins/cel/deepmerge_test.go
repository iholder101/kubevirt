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
 * Copyright The KubeVirt Authors.
 *
 */

package cel_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "kubevirt.io/api/core/v1"

	"libvirt.org/go/libvirtxml"

	celutil "kubevirt.io/kubevirt/pkg/plugins/cel"
)

var _ = Describe("deep merge edge cases", func() {
	var (
		evaluator *celutil.DomainHookEvaluator
		vmi       *v1.VirtualMachineInstance
		domain    *libvirtxml.Domain
	)

	BeforeEach(func() {
		var err error
		evaluator, err = celutil.NewDomainHookEvaluator()
		Expect(err).NotTo(HaveOccurred())

		vmi = &v1.VirtualMachineInstance{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vmi"},
		}
		domain = &libvirtxml.Domain{
			Type: "kvm",
			Name: "test-vm",
			Memory: &libvirtxml.DomainMemory{
				Value: 1024,
				Unit:  "MiB",
			},
			Devices: &libvirtxml.DomainDeviceList{
				Disks: []libvirtxml.DomainDisk{
					{Device: "disk"},
				},
			},
		}
	})

	Context("nil pointer initialization", func() {
		It("should initialize deeply nested nil pointer chain", func() {
			domain.OS = nil
			expr := `Domain{OS: DomainOS{Type: DomainOSType{Type: "hvm"}}}`
			result, err := evaluator.EvaluateMutation(expr, vmi, domain)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.OS).NotTo(BeNil())
			Expect(result.OS.Type).NotTo(BeNil())
			Expect(result.OS.Type.Type).To(Equal("hvm"))
		})

		It("should initialize nil pointer and preserve sibling fields", func() {
			domain.CPU = nil
			expr := `Domain{CPU: DomainCPU{Mode: "host-passthrough"}}`
			result, err := evaluator.EvaluateMutation(expr, vmi, domain)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.CPU).NotTo(BeNil())
			Expect(result.CPU.Mode).To(Equal("host-passthrough"))
			Expect(result.Name).To(Equal("test-vm"))
			Expect(result.Memory).NotTo(BeNil())
		})
	})

	Context("empty slices", func() {
		It("should replace existing items with empty slice", func() {
			domain.SysInfo = []libvirtxml.DomainSysInfo{{SMBIOS: &libvirtxml.DomainSysInfoSMBIOS{}}}
			expr := `Domain{SysInfo: []}`
			result, err := evaluator.EvaluateMutation(expr, vmi, domain)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.SysInfo).To(BeEmpty())
		})

		It("should set a slice on a base that had none", func() {
			domain.SysInfo = nil
			expr := `Domain{SysInfo: [DomainSysInfo{SMBIOS: DomainSysInfoSMBIOS{}}]}`
			result, err := evaluator.EvaluateMutation(expr, vmi, domain)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.SysInfo).To(HaveLen(1))
		})
	})

	Context("nested struct partial merge", func() {
		It("should merge only specified nested fields and preserve others", func() {
			domain.Memory = &libvirtxml.DomainMemory{Value: 1024, Unit: "MiB"}
			expr := `Domain{Memory: DomainMemory{Unit: "GiB"}}`
			result, err := evaluator.EvaluateMutation(expr, vmi, domain)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Memory.Unit).To(Equal("GiB"))
			Expect(result.Memory.Value).To(Equal(uint(1024)))
		})

		It("should handle multiple levels of nesting", func() {
			expr := `Domain{
				Devices: DomainDeviceList{
					Emulator: "/usr/bin/qemu-system-x86_64"
				}
			}`
			result, err := evaluator.EvaluateMutation(expr, vmi, domain)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Devices.Emulator).To(Equal("/usr/bin/qemu-system-x86_64"))
			Expect(result.Devices.Disks).To(HaveLen(1))
		})
	})

	Context("multiple fields in single mutation", func() {
		It("should set multiple top-level fields at once", func() {
			expr := `Domain{Title: "new-title", Description: "a description"}`
			result, err := evaluator.EvaluateMutation(expr, vmi, domain)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Title).To(Equal("new-title"))
			Expect(result.Description).To(Equal("a description"))
			Expect(result.Name).To(Equal("test-vm"))
		})

		It("should set nested and top-level fields together", func() {
			expr := `Domain{
				Title: "titled",
				CPU: DomainCPU{Mode: "custom"}
			}`
			result, err := evaluator.EvaluateMutation(expr, vmi, domain)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Title).To(Equal("titled"))
			Expect(result.CPU).NotTo(BeNil())
			Expect(result.CPU.Mode).To(Equal("custom"))
		})
	})

	Context("null values", func() {
		It("should clear a pointer field when set to null", func() {
			domain.Memory = &libvirtxml.DomainMemory{Value: 1024, Unit: "MiB"}
			expr := `Domain{Memory: null}`
			result, err := evaluator.EvaluateMutation(expr, vmi, domain)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Memory).To(BeNil())
		})
	})
})
