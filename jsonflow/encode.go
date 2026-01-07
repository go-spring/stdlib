/*
 * Copyright 2025 The Go-Spring Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package jsonflow

import (
	"github.com/go-spring/stdlib/jsonflow/internal/json"
)

// NotForPublicUse is a private type used to prevent the use of
// the package outside of this module.
type NotForPublicUse struct{}

// MarshalOptions is an interface that defines options for encoding JSON.
type MarshalOptions interface {
	JSONOptions(NotForPublicUse)
}

type (
	Indent         string
	IndentPrefix   string
	NilSliceAsNull bool
	NilMapAsNull   bool
	Deterministic  bool
)

func (Indent) JSONOptions(NotForPublicUse)         {}
func (IndentPrefix) JSONOptions(NotForPublicUse)   {}
func (NilSliceAsNull) JSONOptions(NotForPublicUse) {}
func (NilMapAsNull) JSONOptions(NotForPublicUse)   {}
func (Deterministic) JSONOptions(NotForPublicUse)  {}

// Encoder is a streaming JSON encoder.
type Encoder = json.Encoder

// EncodeInt encodes an integer value to JSON.
func EncodeInt[T ~int | ~int8 | ~int16 | ~int32 | ~int64](e Encoder, i T) error {
	return nil
}
