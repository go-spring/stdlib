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

// Package flatten provides utilities for validating the *structure* of
// hierarchical configuration data by converting it into a flat key space.
//
// The primary design goal of this package is *structural key validation* for
// structured configuration formats such as JSON, YAML, or TOML. Rather than
// operating directly on nested maps and slices via reflection, flatten converts
// hierarchical data into a flat key/value representation while retaining enough
// structural metadata to:
//
//   - Validate key paths against structural constraints
//   - Detect property conflicts early (e.g. map vs array, value vs container)
//   - Support deterministic traversal and querying
//   - Track value provenance across multiple configuration sources
//
// Flattened keys use:
//
//   - Dot notation for map/object fields:     "db.host"
//   - Index notation for arrays/slices:       "servers[0].port"
//
// For example:
//
//	{"db": {"hosts": ["a", "b"]}}
//
// becomes:
//
//	{
//	  "db.hosts[0]": "a",
//	  "db.hosts[1]": "b",
//	}
//
// Internally, the package deliberately separates *structure* from *values*:
//
//   - Structure is tracked by an internal hierarchical tree that models paths
//     as typed segments (map keys or array indices) and enforces consistency.
//   - Leaf values are stored in flat maps keyed by normalized string paths.
//
// This separation allows flatten to perform strict structural validation
// without duplicating data, while still providing a simple flat representation
// for querying, comparison, merging, and diffing.
//
// Key components include:
//
//   - Path: a typed abstraction over hierarchical keys, supporting parsing
//     from and formatting to string paths such as "foo.bar[0]".
//   - Storage: a container for flattened key/value pairs that maintains the
//     internal structure tree, prevents conflicting writes, and associates
//     values with their source files for provenance tracking.
//   - Query helpers: utilities for existence checks, subkey enumeration, and
//     deterministic iteration.
//
// Typical use cases:
//
//   - Normalizing configuration files from multiple sources for comparison,
//     merging, or diffing.
//   - Querying deeply nested configuration data using simple string paths
//     without dealing with reflection or nested map structures directly.
//   - Building configuration tooling that requires strict structural guarantees
//     and reproducible traversal order.
package flatten

import (
	"io"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/go-spring/stdlib/errutil"
	"github.com/go-spring/stdlib/listutil"
	"github.com/go-spring/stdlib/ordered"
)

// treeNode represents a structural node in the internal hierarchy tree.
//
// A treeNode exists solely to model *structure*, not to store values.
// Each node corresponds to a single path segment and is typed as either:
//
//   - a map/object key
//   - an array index
//
// Invariants:
//
//   - treeNode never stores leaf values
//   - leaf values are stored exclusively in Storage.data or Storage.empty
//   - the Type of a node determines the expected type of all its children
//   - a nil *treeNode represents a leaf position (no further structure)
//
// This design allows early detection of invalid configurations such as:
//
//   - treating the same path as both a map and an array
//   - assigning a value where a container already exists
type treeNode struct {
	Type PathType
	Data map[string]*treeNode
}

// ValueInfo stores a flattened value together with its source information.
//
// The File field records the Storage-local numeric identifier of the
// configuration file from which the value originated. This enables
// provenance tracking and deterministic merging behavior across multiple inputs.
type ValueInfo struct {
	File  int8
	Value string
}

// Storage is the central data structure of this package.
//
// It maintains three logically distinct layers:
//
//  1. root  – a hierarchical tree that models *only structure*
//  2. data  – flattened leaf key/value pairs
//  3. empty – flattened keys representing empty containers or nil values
//
// empty is tracked separately to preserve leaf semantics for empty
// containers and to prevent illegal path extension.
//
// Additionally, Storage tracks file provenance using a compact int8 index.
//
// Core invariants:
//
//   - root contains no values, only structure
//   - data contains only concrete leaf values
//   - empty contains only leaf paths representing [], {}, or <nil>
//   - a single path cannot simultaneously be a container and a value
type Storage struct {
	root  *treeNode
	data  map[string]ValueInfo
	empty map[string]ValueInfo
	file  map[string]int8
}

// NewStorage creates a new Storage instance.
func NewStorage() *Storage {
	return &Storage{
		data:  make(map[string]ValueInfo),
		empty: make(map[string]ValueInfo),
		file:  make(map[string]int8),
	}
}

// Keys returns all flattened keys currently stored in the Storage.
//
// This includes both concrete values and empty-container markers.
// The result is sorted lexicographically to ensure deterministic iteration.
func (s *Storage) Keys() []string {
	var keys []string
	keys = slices.AppendSeq(keys, maps.Keys(s.data))
	keys = slices.AppendSeq(keys, maps.Keys(s.empty))
	sort.Strings(keys)
	return keys
}

// Data returns all flattened key/value pairs currently stored in the Storage.
//
// The result includes both concrete values and empty-container markers
// (e.g. "[]", "{}", "<nil>"), and is sufficient to reconstruct the Storage
// structure when re-inserted via Set.
func (s *Storage) Data() map[string]string {
	r := make(map[string]string)
	for _, m := range listutil.SliceOf(s.data, s.empty) {
		for k, v := range m {
			r[k] = v.Value
		}
	}
	return r
}

// AddFile registers a configuration source and assigns it a compact int8 ID.
//
// If the file has already been registered, the existing ID is returned.
//
// The total number of files is limited to 127, which is considered sufficient
// for typical configuration-merging scenarios.
func (s *Storage) AddFile(file string) int8 {
	if i, ok := s.file[file]; ok {
		return i
	}
	i := int8(len(s.file))
	s.file[file] = i
	return i
}

// MergeMap flattens a nested map and inserts all resulting key/value pairs
// into the Storage under the given file identity.
//
// Structural conflicts are detected eagerly during insertion.
func (s *Storage) MergeMap(data map[string]any, file string) error {
	fileID := s.AddFile(file)
	for key, val := range Flatten(data) {
		if err := s.Set(key, val, fileID); err != nil {
			return err
		}
	}
	return nil
}

// Merge imports all values from another Storage instance.
//
// File identities from the source Storage are remapped to local IDs while
// preserving provenance semantics.
//
// Note: When merging Storages with identical filenames, the provenance
// information may become ambiguous as files with the same name from
// different sources will share the same file ID in the merged result.
func (s *Storage) Merge(p *Storage) error {

	// source_filename -> target_file_id
	newFileIDMap := make(map[string]int8)
	for file := range p.file {
		newFileIDMap[file] = s.AddFile(file)
	}

	// source_file_id -> source_filename
	oldFileIDMap := make(map[int8]string)
	for file, fileID := range p.file {
		oldFileIDMap[fileID] = file
	}

	for _, m := range listutil.SliceOf(p.data, p.empty) {
		for key, r := range m {
			// source_file_id -> source_filename -> target_file_id
			fileID := newFileIDMap[oldFileIDMap[r.File]]
			if err := s.Set(key, r.Value, fileID); err != nil {
				return err
			}
		}
	}
	return nil
}

// Lookup retrieves the value associated with a flattened key.
//
// The key must refer to a concrete leaf value.
// Intermediate nodes and empty-container markers are intentionally excluded.
func (s *Storage) Lookup(key string) (string, bool) {
	if v, ok := s.data[key]; ok {
		return v.Value, true
	}
	return "", false
}

// Get retrieves the value associated with a flattened key, or returns a default.
//
// Only concrete leaf values are considered valid lookup targets.
// If the key does not exist, the first provided default value (if any) is returned.
func (s *Storage) Get(key string, def ...string) string {
	if v, ok := s.data[key]; ok {
		return v.Value
	}
	if len(def) == 0 {
		return ""
	}
	return def[0]
}

// checkNode validates that the current path segment is compatible with
// the existing structural tree, and reports any structural conflicts.
func checkNode(s *Storage, n *treeNode, pathNode Path, path []Path, i int) error {
	if n == nil {
		tempPath := JoinPath(path[:i+1])

		// The path previously terminated at an empty container (e.g. a.b = []),
		// and the caller is now attempting to extend it (e.g. a.b.c = 3).
		if _, ok := s.empty[tempPath]; ok {
			return errutil.Explain(nil, "cannot extend path %s: empty container is a leaf", tempPath)
		}

		// The path previously terminated at a concrete value (e.g. a.b = 1),
		// and the caller is now attempting to create a child node (e.g. a.b.c = 2).
		if _, ok := s.data[tempPath]; ok {
			return errutil.Explain(nil, "cannot extend path %s: value is a leaf", tempPath)
		}

		// This branch should be unreachable under normal invariants.
		// It is kept as a defensive fallback in case of internal inconsistency.
		return errutil.Explain(nil, "path %s conflicts with existing structure", tempPath)
	}
	if pathNode.Type != n.Type {
		// The expected path segment type (map key vs array index) does not
		// match the existing structural node type, indicating a type conflict
		// such as treating the same path as both a map and an array.
		return errutil.Explain(nil, "type conflict at path %s: expect %s but found %s",
			JoinPath(path[:i+1]), pathNode.Type, n.Type)
	}
	return nil
}

// Set inserts or updates a flattened key/value pair while enforcing
// structural consistency.
//
// During insertion, the key path is validated against the internal tree to
// ensure that:
//
//   - a value is not written where a container already exists
//   - a map branch is not reinterpreted as an array branch (or vice versa)
//   - no partial prefix of the key conflicts with existing structure
//
// Any structural violation results in an immediate error.
func (s *Storage) Set(key string, val string, file int8) error {
	if key == "" {
		return errutil.Explain(nil, "key is empty")
	}

	path, err := SplitPath(key)
	if err != nil {
		return err
	}

	// Initialize the root node on first insertion.
	// The root type is fixed and must remain consistent thereafter.
	if s.root == nil {
		s.root = &treeNode{
			Type: path[0].Type,
			Data: make(map[string]*treeNode),
		}
	}

	n := s.root
	for i, pathNode := range path {
		if err = checkNode(s, n, pathNode, path, i); err != nil {
			return err
		}
		v, ok := n.Data[pathNode.Elem]
		if !ok {
			if i < len(path)-1 {
				v = &treeNode{
					Type: path[i+1].Type,
					Data: make(map[string]*treeNode),
				}
			}
			n.Data[pathNode.Elem] = v
		}
		n = v
	}

	if n != nil {
		// A structural node already exists at this path, which means the key
		// refers to a container (map or array). Overwriting a container with
		// a leaf value is not allowed.
		return errutil.Explain(nil, "cannot overwrite path %s", key)
	}

	// Store the value or empty container
	switch val {
	case "[]", "{}", "<nil>":
		if _, ok := s.data[key]; ok {
			return errutil.Explain(nil, "cannot overwrite path %s", key)
		}
		// Value overwrites are allowed to support configuration merging.
		s.empty[key] = ValueInfo{file, val}
	default:
		if _, ok := s.empty[key]; ok {
			return errutil.Explain(nil, "cannot overwrite path %s", key)
		}
		// Value overwrites are allowed to support configuration merging.
		s.data[key] = ValueInfo{file, val}
	}
	return nil
}

// Exists determines whether a key or path *structurally exists* within the Storage.
//
// This check is intentionally permissive: the key does not need to represent
// a valid leaf path. Container nodes (including arrays) and intermediate
// structural nodes are considered existing as long as they are compatible
// with the current structure.
func (s *Storage) Exists(key string) bool {
	if key == "" || s.root == nil {
		return false
	}

	if _, ok := s.empty[key]; ok {
		return true
	}

	if _, ok := s.data[key]; ok {
		return true
	}

	// Invalid paths are treated as non-existent.
	path, err := SplitPath(key)
	if err != nil {
		return false
	}

	n := s.root
	for i, pathNode := range path {
		if err = checkNode(s, n, pathNode, path, i); err != nil {
			return false
		}
		v, ok := n.Data[pathNode.Elem]
		if !ok {
			return false
		}
		n = v
	}
	return true
}

// SubKeys returns the immediate child keys of a container path.
//
// The path may refer to either a map or an array; child keys are returned
// uniformly as strings (map keys or numeric indices).
//
// Behavior:
//
//   - If the path refers to a leaf value, an error is returned
//   - If the path does not exist, nil is returned
//   - If the path refers to an empty container, an empty slice is returned
//
// An empty key indicates traversal starting from the root node.
func (s *Storage) SubKeys(key string) (_ []string, err error) {
	var path []Path
	if key != "" {
		if path, err = SplitPath(key); err != nil {
			return nil, err
		}
	}

	// Not initialized yet
	if s.root == nil {
		return nil, nil
	}

	if _, ok := s.empty[key]; ok {
		return nil, nil
	}

	if _, ok := s.data[key]; ok {
		// Leaf values do not have child keys. Attempting to enumerate
		// subkeys on a leaf indicates a misuse of the API.
		return nil, errutil.Explain(nil, "cannot list subkeys of leaf value at path %s", key)
	}

	n := s.root
	for i, pathNode := range path {
		if err = checkNode(s, n, pathNode, path, i); err != nil {
			return nil, err
		}
		v, ok := n.Data[pathNode.Elem]
		if !ok {
			return nil, nil
		}
		n = v
	}
	return ordered.MapKeys(n.Data), nil
}

// SubTree extracts all descendant key/value pairs under the given key.
//
// Returned keys have the prefix removed. The result may include empty
// container markers ([] / {} / <nil>), allowing reconstruction of a
// Storage instance if desired.
//
// The key must not be empty.
func (s *Storage) SubTree(key string) (map[string]string, error) {
	if key == "" {
		return nil, errutil.Explain(nil, "key is empty")
	}

	// Validate the key. In particular, leaf values cannot have subtrees,
	// and any structural conflict should be reported as an error.
	if _, err := s.SubKeys(key); err != nil {
		return nil, err
	}

	r := make(map[string]string)
	for _, m := range listutil.SliceOf(s.data, s.empty) {
		for k, v := range m {
			str, ok := strings.CutPrefix(k, key)
			if !ok || str == "" {
				continue
			}
			if c := str[0]; c != '.' && c != '[' {
				continue
			}
			r[str[1:]] = v.Value
		}
	}
	return r, nil
}

// Dump writes the contents of Storage to the given writer in a
// human-readable, deterministic format grouped by source file.
//
// The output is intended for inspection and debugging purposes only.
// It is not a stable serialization format and should not be parsed
// or relied upon for programmatic consumption.
func (s *Storage) Dump(w io.Writer) error {
	keys := s.Keys()
	for _, file := range ordered.MapKeys(s.file) {
		if err := listutil.WriteStrings(w, file, ":\n"); err != nil {
			return errutil.Explain(err, "failed to dump")
		}
		fileID := s.file[file]
		for _, key := range keys {
			r, ok := s.data[key]
			if !ok {
				r, ok = s.empty[key]
				if !ok {
					continue
				}
			}
			if r.File != fileID {
				continue
			}
			if err := listutil.WriteStrings(w, "  ", key, "=", r.Value, "\n"); err != nil {
				return errutil.Explain(err, "failed to dump")
			}
		}
	}
	return nil
}
