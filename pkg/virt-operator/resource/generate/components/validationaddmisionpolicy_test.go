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

package components_test

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/admission/plugin/cel"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/cel/environment"

	celgo "github.com/google/cel-go/cel"
	celtypes "github.com/google/cel-go/common/types"

	"kubevirt.io/kubevirt/pkg/virt-operator/resource/generate/components"
)

var _ = Describe("Validation Admission Policy", func() {
	Context("ValidatingAdmissionPolicyBinding", func() {
		It("should generate the expected policy binding", func() {
			const userName = "system:serviceaccount:kubevirt-ns:kubevirt-handler"
			validatingAdmissionPolicy := components.NewHandlerV1ValidatingAdmissionPolicy(userName)
			validatingAdmissionPolicyBinding := components.NewHandlerV1ValidatingAdmissionPolicyBinding()

			Expect(validatingAdmissionPolicyBinding.Spec.PolicyName).To(Equal(validatingAdmissionPolicy.Name))
			Expect(validatingAdmissionPolicyBinding.Kind).ToNot(BeEmpty())
		})
	})
	Context("ValidatingAdmissionPolicy", func() {
		It("should generate the expected policy", func() {
			const userName = "system:serviceaccount:kubevirt-ns:kubevirt-handler"
			validatingAdmissionPolicy := components.NewHandlerV1ValidatingAdmissionPolicy(userName)

			expectedMatchConditionExpression := fmt.Sprintf("request.userInfo.username == %q", userName)
			Expect(validatingAdmissionPolicy.Spec.MatchConditions[0].Expression).To(Equal(expectedMatchConditionExpression))
			Expect(validatingAdmissionPolicy.Kind).ToNot(BeEmpty())
		})
		Context("Validation Compile test", func() {
			var celCompiler *cel.CompositedCompiler
			BeforeEach(func() {
				compositionEnvTemplateWithoutStrictCost, err := cel.NewCompositionEnv(cel.VariablesTypeName, environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion()))
				Expect(err).ToNot(HaveOccurred())
				celCompiler = cel.NewCompositedCompilerFromTemplate(compositionEnvTemplateWithoutStrictCost)
			})

			It("succeed compiling all the policy validations", func() {
				const userName = "system:serviceaccount:kubevirt-ns:kubevirt-handler"
				validatingAdmissionPolicy := components.NewHandlerV1ValidatingAdmissionPolicy(userName)

				options := cel.OptionalVariableDeclarations{
					HasParams:     false,
					HasAuthorizer: false,
				}
				mode := environment.NewExpressions
				celCompiler.CompileAndStoreVariables(convertV1Variables(validatingAdmissionPolicy.Spec.Variables), options, mode)

				for _, validation := range validatingAdmissionPolicy.Spec.Validations {
					compilationResult := celCompiler.CompileCELExpression(convertV1Validation(validation), options, mode)
					Expect(compilationResult).ToNot(BeNil())
					Expect(compilationResult.Error).To(BeNil())
				}
			})
		})
		Context("Validation Filter test", func() {
			var celCompiler *cel.CompositedCompiler
			BeforeEach(func() {
				compositionEnvTemplateWithoutStrictCost, err := cel.NewCompositionEnv(cel.VariablesTypeName, environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion()))
				Expect(err).ToNot(HaveOccurred())
				celCompiler = cel.NewCompositedCompilerFromTemplate(compositionEnvTemplateWithoutStrictCost)
			})
			It("should fail patching the node with non-kubevirt label", func() {
				const userName = "system:serviceaccount:kubevirt-ns:kubevirt-handler"
				validatingAdmissionPolicy := components.NewHandlerV1ValidatingAdmissionPolicy(userName)

				//replace variables if all else fails
				for idx, _ := range validatingAdmissionPolicy.Spec.Validations {
					for _, variable := range validatingAdmissionPolicy.Spec.Variables {
						validatingAdmissionPolicy.Spec.Validations[idx].Expression = strings.ReplaceAll(validatingAdmissionPolicy.Spec.Validations[idx].Expression, "variables."+variable.Name, variable.Expression)
					}
				}
				options := cel.OptionalVariableDeclarations{
					HasParams:     false,
					HasAuthorizer: false,
				}
				mode := environment.NewExpressions
				celCompiler.CompileAndStoreVariables(convertV1Variables(validatingAdmissionPolicy.Spec.Variables), options, mode)

				var expressions []cel.ExpressionAccessor
				for _, validation := range validatingAdmissionPolicy.Spec.Validations {
					expressions = append(expressions, convertV1Validation(validation))
				}
				filterResults := celCompiler.FilterCompiler.Compile(expressions, options, mode)
				Expect(filterResults.CompilationErrors()).To(HaveLen(0))

				userInfo := &user.DefaultInfo{
					Name: userName,
				}
				const nodeName = "node01"
				oldNode := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:        nodeName,
						Labels:      map[string]string{"label1": "val1"},
						Annotations: map[string]string{"annotations1": "val1"},
					},
					Spec: corev1.NodeSpec{
						Unschedulable: false,
					},
				}
				newNode := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:        nodeName,
						Labels:      map[string]string{"label1": "val1", "kubevirt.io/permittedLabel": ""},
						Annotations: map[string]string{"annotations1": "val1"},
					},
					Spec: corev1.NodeSpec{
						Unschedulable: false,
					},
				}
				nodeAttribiute := admission.NewAttributesRecord(
					oldNode,
					newNode,
					corev1.SchemeGroupVersion.WithKind("Node"),
					corev1.NamespaceAll,
					nodeName,
					corev1.SchemeGroupVersion.WithResource("nodes"),
					"",
					admission.Update,
					&metav1.CreateOptions{},
					false,
					userInfo,
				)
				versionedAttr, err := admission.NewVersionedAttributes(nodeAttribiute, nodeAttribiute.GetKind(), newObjectInterfacesForTest())
				Expect(err).ToNot(HaveOccurred())

				optionalVars := cel.OptionalVariableBindings{}
				evalResults, _, err := filterResults.ForInput(
					context.TODO(),
					versionedAttr,
					cel.CreateAdmissionRequest(versionedAttr.Attributes, metav1.GroupVersionResource(versionedAttr.GetResource()), metav1.GroupVersionKind(versionedAttr.VersionedKind)),
					optionalVars,
					nil,
					celconfig.RuntimeCELCostBudget)
				Expect(err).ToNot(HaveOccurred())

				//varsPrettyVars, err := json.MarshalIndent(celCompiler.CompositionEnv.CompiledVariables, "", "\t")
				//Expect(err).NotTo(HaveOccurred())
				//dataPrettyJSON, err := json.MarshalIndent(evalResults, "", "\t")
				//Expect(err).NotTo(HaveOccurred())
				//fmt.Printf("\ncelCompiler.CompositionEnv.CompiledVariables = %v\nevalResults = \n%s\n\n", string(varsPrettyVars), string(dataPrettyJSON))

				for resultIdx := range evalResults {
					result := evalResults[resultIdx]
					validation := validatingAdmissionPolicy.Spec.Validations[resultIdx]
					Expect(result.Error).To(BeNil(), fmt.Sprintf("validation policy expression %q failed", result.ExpressionAccessor.GetExpression()))
					Expect(result.EvalResult).To(Equal(celtypes.True), fmt.Sprintf("validation policy expression %q returned false. reason: %q", result.ExpressionAccessor.GetExpression(), validation.Message))
				}

			})
		})
	})

})

// Variable is a named expression for composition.
type Variable struct {
	Name       string
	Expression string
}

func (v *Variable) GetExpression() string {
	return v.Expression
}

func (v *Variable) ReturnTypes() []*celgo.Type {
	return []*celgo.Type{celgo.AnyType, celgo.DynType}
}

func (v *Variable) GetName() string {
	return v.Name
}

func convertV1Variables(variables []admissionregistrationv1.Variable) []cel.NamedExpressionAccessor {
	namedExpressions := make([]cel.NamedExpressionAccessor, len(variables))
	for i, variable := range variables {
		namedExpressions[i] = &Variable{Name: variable.Name, Expression: variable.Expression}
	}
	return namedExpressions
}

// ValidationCondition contains the inputs needed to compile, evaluate and validate a cel expression
type ValidationCondition struct {
	Expression string
	Message    string
	Reason     *metav1.StatusReason
}

func (v *ValidationCondition) GetExpression() string {
	return v.Expression
}

func (v *ValidationCondition) ReturnTypes() []*celgo.Type {
	return []*celgo.Type{celgo.BoolType}
}

func convertV1Validation(validation admissionregistrationv1.Validation) cel.ExpressionAccessor {
	return &ValidationCondition{
		Expression: validation.Expression,
		Message:    validation.Message,
		Reason:     validation.Reason,
	}
}

// newObjectInterfacesForTest returns an ObjectInterfaces appropriate for test cases in this file.
func newObjectInterfacesForTest() admission.ObjectInterfaces {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	return admission.NewObjectInterfacesFromScheme(scheme)
}
