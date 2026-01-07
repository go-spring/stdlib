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
	"encoding/json/jsontext"
	stdjsonv2 "encoding/json/v2"
	"io"

	"github.com/go-spring/stdlib/jsonflow/internal/json"
	"github.com/go-spring/stdlib/jsonflow/internal/jsonv2"
)

// NewEncoder creates a new jsonv2.Encoder that implements the json.Encoder interface.
func NewEncoder(w io.Writer) json.Encoder {
	return &jsonv2.Encoder{Encoder: jsontext.NewEncoder(w)}
}

// NewDecoder creates a new jsonv2.Decoder that implements the json.Decoder interface.
func NewDecoder(r io.Reader) json.Decoder {
	return &jsonv2.Decoder{Decoder: jsontext.NewDecoder(r)}
}

// toJSONv2Options converts MarshalOptions to jsontext.Options.
func toJSONv2Options(opts []MarshalOptions) []jsontext.Options {

	// 默认配置
	opts = append([]MarshalOptions{
		NilSliceAsNull(true),
		NilMapAsNull(true),
		Deterministic(true),
	}, opts...)

	var ret []jsontext.Options
	for _, opt := range opts {
		switch x := opt.(type) {
		case Indent:
			ret = append(ret, jsontext.WithIndent(string(x)))
		case IndentPrefix:
			ret = append(ret, jsontext.WithIndentPrefix(string(x)))
		case NilSliceAsNull:
			ret = append(ret, stdjsonv2.FormatNilSliceAsNull(bool(x)))
		case NilMapAsNull:
			ret = append(ret, stdjsonv2.FormatNilMapAsNull(bool(x)))
		case Deterministic:
			ret = append(ret, stdjsonv2.Deterministic(bool(x)))
		default: // for linter
		}
	}
	return ret
}

// Marshal marshals a Go value into JSON bytes.
func Marshal(i any, opts ...MarshalOptions) ([]byte, error) {
	return stdjsonv2.Marshal(i, toJSONv2Options(opts)...)
}

// MarshalIndent marshals a Go value into JSON bytes with indentation.
func MarshalIndent(i any, prefix, indent string) ([]byte, error) {
	return Marshal(i, IndentPrefix(prefix), Indent(indent))
}

// MarshalWrite marshals a Go value into JSON bytes and writes them to a writer.
func MarshalWrite(w io.Writer, i any, opts ...MarshalOptions) error {
	return stdjsonv2.MarshalWrite(w, i, toJSONv2Options(opts)...)
}

// Unmarshal unmarshals JSON bytes into a Go value.
func Unmarshal(b []byte, i any) error {
	return stdjsonv2.Unmarshal(b, i)
}

// UnmarshalRead unmarshals JSON bytes from a reader into a Go value.
func UnmarshalRead(r io.Reader, i any) error {
	return stdjsonv2.UnmarshalRead(r, i)
}
