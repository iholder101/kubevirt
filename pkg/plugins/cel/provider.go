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

// Package cel provides a CEL evaluation engine for plugin domain hooks.
//
// It delegates type registration to cel-go's ext.NativeTypes, which handles
// recursive discovery of all 300+ libvirtxml struct types. The only custom
// behavior is a thin provider wrapper that overrides NewValue() to return
// sparse objectVal values instead of full Go structs - this tracks which
// fields a plugin explicitly set, so deep merge only touches those fields.
//
// Data flow:
//
//	Go struct -> NativeTypes adapter -> CEL evaluation -> sparse objectVal -> deep merge into Go struct
package cel

import (
	"fmt"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// sparseProvider embeds types.Provider, overriding NewValue and
// FindStructFieldType. All other type operations delegate to the
// embedded provider (backed by ext.NativeTypes).
type sparseProvider struct {
	types.Provider
}

// FindStructFieldType delegates type lookup to NativeTypes but wraps the field
// accessors to handle both value types that coexist at runtime:
//   - objectVal (from NewValue): sparse mutation results - read from field map
//   - nativeObj (from NativeTypes adapter): Go struct inputs like domainSpec -
//     delegate to the original accessor which uses reflect
func (p *sparseProvider) FindStructFieldType(typeName, fieldName string) (*types.FieldType, bool) {
	ft, found := p.Provider.FindStructFieldType(typeName, fieldName)
	if !found {
		return nil, false
	}
	return &types.FieldType{
		Type: ft.Type,
		IsSet: func(target any) bool {
			if ov, ok := target.(*objectVal); ok {
				_, present := ov.fields[fieldName]
				return present
			}
			return ft.IsSet(target)
		},
		GetFrom: func(target any) (any, error) {
			if ov, ok := target.(*objectVal); ok {
				val, present := ov.fields[fieldName]
				if !present {
					return nil, fmt.Errorf("field '%s' not set", fieldName)
				}
				return val, nil
			}
			return ft.GetFrom(target)
		},
	}, true
}

// NewValue returns a sparse objectVal that only tracks the explicitly
// provided fields. This is the only method that differs from NativeTypes,
// which would create a full zero-valued Go struct via reflect.New().
func (p *sparseProvider) NewValue(typeName string, fields map[string]ref.Val) ref.Val {
	return &objectVal{
		typeName: typeName,
		fields:   fields,
	}
}
