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

package cel

import (
	"encoding/xml"
	"fmt"
	"reflect"

	"github.com/google/cel-go/cel"
	v1 "kubevirt.io/api/core/v1"

	"libvirt.org/go/libvirtxml"
)

type DomainHookEvaluator struct {
	*Evaluator
	mutationEnv *cel.Env
}

func NewDomainHookEvaluator() (*DomainHookEvaluator, error) {
	eval, err := NewEvaluator(
		WithNativeTypes(reflect.TypeFor[*libvirtxml.Domain]()),
		WithContainer("libvirtxml"),
		WithVariable("domainSpec", cel.ObjectType("libvirtxml.Domain")),
	)
	if err != nil {
		return nil, fmt.Errorf("creating condition evaluator: %w", err)
	}

	wrapper := &sparseProvider{Provider: eval.Env().CELTypeProvider()}
	mutEnv, err := eval.Env().Extend(
		cel.CustomTypeProvider(wrapper),
	)
	if err != nil {
		return nil, fmt.Errorf("creating mutation environment: %w", err)
	}

	return &DomainHookEvaluator{
		Evaluator:   eval,
		mutationEnv: mutEnv,
	}, nil
}

func (e *DomainHookEvaluator) EvaluateCondition(expr string, vmi *v1.VirtualMachineInstance, domain *libvirtxml.Domain) (bool, error) {
	return e.Evaluator.EvaluateCondition(expr, map[string]any{
		"vmi":        vmi,
		"domainSpec": domain,
	})
}

func (e *DomainHookEvaluator) EvaluateMutation(expr string, vmi *v1.VirtualMachineInstance, domain *libvirtxml.Domain) (*libvirtxml.Domain, error) {
	ast, issues := e.mutationEnv.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compiling expression: %w", issues.Err())
	}

	prg, err := e.mutationEnv.Program(ast, cel.CostLimit(costLimit))
	if err != nil {
		return nil, fmt.Errorf("creating program: %w", err)
	}

	out, _, err := prg.Eval(map[string]any{
		"vmi":        vmi,
		"domainSpec": domain,
	})
	if err != nil {
		return nil, fmt.Errorf("evaluating expression: %w", err)
	}

	partial, ok := out.(*objectVal)
	if !ok {
		return nil, fmt.Errorf("mutation must return a Domain object, got %T", out)
	}

	result, err := copyDomain(domain)
	if err != nil {
		return nil, fmt.Errorf("copying domain: %w", err)
	}
	if err := deepMerge(reflect.ValueOf(result).Elem(), partial); err != nil {
		return nil, fmt.Errorf("merging mutation result: %w", err)
	}
	return result, nil
}

func (e *DomainHookEvaluator) CompileMutation(expr string) error {
	ast, issues := e.mutationEnv.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return fmt.Errorf("compiling expression: %w", issues.Err())
	}
	if !ast.OutputType().IsEquivalentType(cel.ObjectType("libvirtxml.Domain")) {
		return fmt.Errorf("mutation must return Domain, got %s", ast.OutputType())
	}
	return nil
}

func copyDomain(src *libvirtxml.Domain) (*libvirtxml.Domain, error) {
	xmlBytes, err := xml.Marshal(src)
	if err != nil {
		return nil, fmt.Errorf("marshaling domain for copy: %w", err)
	}
	dst := &libvirtxml.Domain{}
	if err := xml.Unmarshal(xmlBytes, dst); err != nil {
		return nil, fmt.Errorf("unmarshaling domain for copy: %w", err)
	}
	return dst, nil
}
