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

package flatten

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-spring/stdlib/testing/assert"
)

func TestStorage(t *testing.T) {

	t.Run("basic_operations", func(t *testing.T) {
		s := NewStorage()
		fileID := s.AddFile("test.go")

		_, err := s.CheckKey("")
		assert.Error(t, err).Matches("key is empty")
		exists, err := s.CheckKey("nonexistent")
		assert.That(t, err).Nil()
		assert.That(t, exists).False()

		subKeys, err := s.SubKeys("")
		assert.That(t, err).Nil()
		assert.That(t, subKeys).Nil()

		subKeys, err = s.SubKeys("any.path")
		assert.That(t, err).Nil()
		assert.That(t, subKeys).Nil()

		keys := s.Keys()
		assert.That(t, keys).Nil()

		assert.Error(t, s.Set("", "value", fileID)).Matches("key is empty")
	})

	t.Run("map_operations", func(t *testing.T) {
		s := NewStorage()
		fileID := s.AddFile("test.go")

		assert.That(t, s.Set("config.host", "localhost", fileID)).Nil()
		exists, err := s.CheckKey("config.host")
		assert.That(t, err).Nil()
		assert.That(t, exists).True()
		assert.That(t, s.Get("config.host")).Equal("localhost")

		assert.Error(t, s.Set("config.host.port", "8080", fileID)).
			Matches("path config.host.port conflicts with existing structure")
		assert.Error(t, s.Set("config[0]", "value", fileID)).
			Matches(`type conflict at path config\[0]: expect index but found key`)

		assert.That(t, s.Set("server.name", "web1", fileID)).Nil()
		assert.That(t, s.Set("server.port", "8080", fileID)).Nil()

		subKeys, err := s.SubKeys("server")
		assert.That(t, err).Nil()
		assert.That(t, subKeys).Equal([]string{"name", "port"})

		_, err = s.SubKeys("config.host")
		assert.Error(t, err).Matches("cannot list subkeys of leaf value at path config.host")

		keys := s.Keys()
		expected := []string{"config.host", "server.name", "server.port"}
		assert.That(t, keys).Equal(expected)
	})

	t.Run("array_operations", func(t *testing.T) {
		s := NewStorage()
		fileID := s.AddFile("test.go")

		assert.That(t, s.Set("servers[0]", "web1", fileID)).Nil()
		assert.That(t, s.Set("servers[1]", "web2", fileID)).Nil()

		exists, err := s.CheckKey("servers[0]")
		assert.That(t, err).Nil()
		assert.That(t, exists).True()

		subKeys, err := s.SubKeys("servers")
		assert.That(t, err).Nil()
		assert.That(t, subKeys).Equal([]string{"0", "1"})

		assert.Error(t, s.Set("servers.host", "localhost", fileID)).
			Matches("type conflict at path servers.host: expect key but found index")

		assert.That(t, s.Get("servers[2]", "default")).Equal("default")
		assert.That(t, s.Get("servers[2]")).Equal("")

		keys := s.Keys()
		assert.That(t, keys).Equal([]string{"servers[0]", "servers[1]"})
	})

	t.Run("complex_nested_structures", func(t *testing.T) {
		s := NewStorage()
		fileID := s.AddFile("test.go")

		// Test deeply nested mixed structure
		assert.That(t, s.Set("database.connections[0].host", "localhost", fileID)).Nil()
		assert.That(t, s.Set("database.connections[0].port", "5432", fileID)).Nil()
		assert.That(t, s.Set("database.connections[1].host", "remote", fileID)).Nil()

		exists, err := s.CheckKey("database.connections[0].host")
		assert.That(t, err).Nil()
		assert.That(t, exists).True()

		subKeys, err := s.SubKeys("database")
		assert.That(t, err).Nil()
		assert.That(t, subKeys).Equal([]string{"connections"})

		subKeys, err = s.SubKeys("database.connections")
		assert.That(t, err).Nil()
		assert.That(t, subKeys).Equal([]string{"0", "1"})

		subKeys, err = s.SubKeys("database.connections[0]")
		assert.That(t, err).Nil()
		assert.That(t, subKeys).Equal([]string{"host", "port"})

		// Test value retrieval
		assert.That(t, s.Get("database.connections[0].host")).Equal("localhost")
		assert.That(t, s.Get("database.connections[99]", "default")).Equal("default")

		keys := s.Keys()
		expected := []string{
			"database.connections[0].host",
			"database.connections[0].port",
			"database.connections[1].host",
		}
		assert.That(t, keys).Equal(expected)
	})

	t.Run("file_management", func(t *testing.T) {
		s := NewStorage()

		fileID1 := s.AddFile("config.json")
		fileID2 := s.AddFile("config.json")
		assert.That(t, fileID1).Equal(fileID2)

		fileID3 := s.AddFile("settings.yaml")
		fileID4 := s.AddFile("defaults.toml")

		assert.That(t, fileID1).Equal(int8(0))
		assert.That(t, fileID3).Equal(int8(1))
		assert.That(t, fileID4).Equal(int8(2))

		// Verify file mapping integrity
		assert.That(t, len(s.file)).Equal(3)
		assert.That(t, s.file["config.json"]).Equal(int8(0))
		assert.That(t, s.file["settings.yaml"]).Equal(int8(1))
	})

	t.Run("merge_operations", func(t *testing.T) {
		s1 := NewStorage()
		s2 := NewStorage()

		fileID1 := s1.AddFile("config1.json")
		err := s1.Set("server.host", "localhost", fileID1)
		assert.Error(t, err).Nil()
		err = s1.Set("server.port", "8080", fileID1)
		assert.Error(t, err).Nil()

		fileID2 := s2.AddFile("config2.json")
		err = s2.Set("server.ssl", "true", fileID2)
		assert.Error(t, err).Nil()
		err = s2.Set("database.url", "postgres://...", fileID2)
		assert.Error(t, err).Nil()

		err = s1.Merge(s2)
		assert.That(t, err).Nil()

		// Verify merged data
		assert.That(t, s1.Get("server.host")).Equal("localhost")
		assert.That(t, s1.Get("server.ssl")).Equal("true")
		assert.That(t, s1.Get("database.url")).Equal("postgres://...")

		// Test file mapping preservation
		assert.That(t, len(s1.file)).Equal(2)
		assert.That(t, s1.file["config1.json"]).Equal(int8(0))
		assert.That(t, s1.file["config2.json"]).Equal(int8(1))

		s3 := NewStorage()
		testMap := map[string]any{
			"logging": map[string]any{
				"level": "info",
				"file":  "/var/log/app.log",
			},
			"features": []string{"auth", "metrics"},
		}

		err = s3.MergeMap(testMap, "dynamic_config.json")
		assert.That(t, err).Nil()

		assert.That(t, s3.Get("logging.level")).Equal("info")
		assert.That(t, s3.Get("features[0]")).Equal("auth")
	})

	t.Run("empty_containers", func(t *testing.T) {
		s := NewStorage()
		fileID := s.AddFile("test.go")

		// Test empty array
		assert.That(t, s.Set("empty_arr", "[]", fileID)).Nil()
		exists, err := s.CheckKey("empty_arr")
		assert.That(t, err).Nil()
		assert.That(t, exists).True()

		_, inData := s.data["empty_arr"]
		_, inEmpty := s.empty["empty_arr"]
		assert.That(t, inData).False()
		assert.That(t, inEmpty).True()

		// Test empty object
		assert.That(t, s.Set("empty_obj", "{}", fileID)).Nil()
		exists, err = s.CheckKey("empty_obj")
		assert.That(t, err).Nil()
		assert.That(t, exists).True()

		_, inData = s.data["empty_obj"]
		_, inEmpty = s.empty["empty_obj"]
		assert.That(t, inData).False()
		assert.That(t, inEmpty).True()

		// Test nil value
		assert.That(t, s.Set("nil_val", "&lt;nil&gt;", fileID)).Nil()
		exists, err = s.CheckKey("nil_val")
		assert.That(t, err).Nil()
		assert.That(t, exists).True()

		_, inData = s.data["nil_val"]
		_, inEmpty = s.empty["nil_val"]
		assert.That(t, inData).True()   // nil value stored in data
		assert.That(t, inEmpty).False() // not in empty

		// Test empty container extension restriction
		assert.Error(t, s.Set("empty_arr[0]", "item", fileID)).NotNil()

		subKeys, err := s.SubKeys("empty_arr")
		assert.That(t, err).Nil()
		assert.That(t, subKeys).Nil()

		subKeys, err = s.SubKeys("empty_obj")
		assert.That(t, err).Nil()
		assert.That(t, subKeys).Nil()
	})

	t.Run("lookup_and_edge_cases", func(t *testing.T) {
		s := NewStorage()
		fileID := s.AddFile("test.go")

		assert.That(t, s.Set("config.api_key", "secret123", fileID)).Nil()

		value, exists := s.Lookup("config.api_key")
		assert.That(t, exists).True()
		assert.That(t, value).Equal("secret123")

		_, exists = s.Lookup("nonexistent")
		assert.That(t, exists).False()

		// Test edge cases for Get with multiple defaults
		assert.That(t, s.Get("missing", "first", "second")).Equal("first")
		assert.That(t, s.Get("missing")).Equal("")

		// Test key with special characters
		assert.That(t, s.Set("config.\"quoted\"", "value", fileID)).Nil()
		assert.That(t, s.Get("config.\"quoted\"")).Equal("value")
	})

	t.Run("subtree_operations", func(t *testing.T) {
		s := NewStorage()
		fileID := s.AddFile("test.go")

		assert.That(t, s.Set("users.admin.name", "Alice", fileID)).Nil()
		assert.That(t, s.Set("users.admin.role", "admin", fileID)).Nil()
		assert.That(t, s.Set("users.guest.name", "Bob", fileID)).Nil()
		assert.That(t, s.Set("settings.debug", "true", fileID)).Nil()

		subtree, err := s.SubTree("users.admin")
		assert.That(t, err).Nil()
		assert.That(t, len(subtree)).Equal(2)
		assert.That(t, subtree["name"]).Equal("Alice")
		assert.That(t, subtree["role"]).Equal("admin")

		subtree, err = s.SubTree("nonexistent")
		assert.That(t, err).Nil()
		assert.That(t, len(subtree)).Equal(0)

		_, err = s.SubTree("")
		assert.Error(t, err).Matches("key is empty")
		_, err = s.SubTree("users.admin.name")
		assert.Error(t, err).Matches("cannot list subkeys of leaf value at path users.admin.name")

		subtree, err = s.SubTree("users")
		assert.That(t, err).Nil()
		assert.That(t, len(subtree)).Equal(3) // admin.name, admin.role, guest.name
	})

	t.Run("dump_functionality", func(t *testing.T) {
		s := NewStorage()
		fileID1 := s.AddFile("config.json")
		fileID2 := s.AddFile("secrets.json")

		err := s.Set("server.host", "localhost", fileID1)
		assert.Error(t, err).Nil()
		err = s.Set("server.port", "8080", fileID1)
		assert.Error(t, err).Nil()
		err = s.Set("database.password", "secret", fileID2)
		assert.Error(t, err).Nil()
		err = s.Set("database.url", "postgres://...", fileID2)
		assert.Error(t, err).Nil()

		var buf bytes.Buffer
		err = s.Dump(&buf)
		assert.That(t, err).Nil()

		output := buf.String()
		// Verify output contains expected content
		assert.That(t, strings.Contains(output, "config.json:")).True()
		assert.That(t, strings.Contains(output, "secrets.json:")).True()
		assert.That(t, strings.Contains(output, "server.host=localhost")).True()
		assert.That(t, strings.Contains(output, "database.password=secret")).True()

		// Test Dump with empty storage
		emptyStorage := NewStorage()
		var emptyBuf bytes.Buffer
		err = emptyStorage.Dump(&emptyBuf)
		assert.That(t, err).Nil()
		assert.That(t, emptyBuf.String()).Equal("")
	})
}
