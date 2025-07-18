/*
Copyright 2025 The bpfman Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func parsePodsArgs(args []string) (nameFilter string, outputJSON bool, err error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--name":
			if i+1 < len(args) {
				nameFilter = args[i+1]
				i++
			}
		case "-o":
			if i+1 < len(args) && args[i+1] == "json" {
				outputJSON = true
				i++
			}
		}
	}

	if !outputJSON {
		return "", false, assert.AnError
	}
	return nameFilter, outputJSON, nil
}

func parsePsArgs(args []string) (podID string, outputJSON bool, err error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--pod":
			if i+1 < len(args) {
				podID = args[i+1]
				i++
			}
		case "-o":
			if i+1 < len(args) && args[i+1] == "json" {
				outputJSON = true
				i++
			}
		}
	}

	if !outputJSON {
		return "", false, assert.AnError
	}
	return podID, outputJSON, nil
}

func parseInspectArgs(args []string) (containerID string, outputJSON bool, err error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o":
			if i+1 < len(args) && args[i+1] == "json" {
				outputJSON = true
				i++
			}
		default:
			if args[i][0] != '-' {
				containerID = args[i]
			}
		}
	}

	if !outputJSON {
		return "", false, assert.AnError
	}
	if containerID == "" {
		return "", false, assert.AnError
	}
	return containerID, outputJSON, nil
}

func TestPodsArgumentParsing(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		expectError  bool
		expectedName string
		expectedJSON bool
	}{
		{
			name:        "missing json output flag",
			args:        []string{"--name", "test-pod"},
			expectError: true,
		},
		{
			name:         "valid args with name filter",
			args:         []string{"--name", "test-pod", "-o", "json"},
			expectError:  false,
			expectedName: "test-pod",
			expectedJSON: true,
		},
		{
			name:         "valid args without name filter",
			args:         []string{"-o", "json"},
			expectError:  false,
			expectedName: "",
			expectedJSON: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, json, err := parsePodsArgs(tt.args)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedName, name)
				assert.Equal(t, tt.expectedJSON, json)
			}
		})
	}
}

func TestPsArgumentParsing(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		expectError  bool
		expectedPod  string
		expectedJSON bool
	}{
		{
			name:        "missing json output flag",
			args:        []string{"--pod", "pod1"},
			expectError: true,
		},
		{
			name:         "valid args",
			args:         []string{"--pod", "pod1", "-o", "json"},
			expectError:  false,
			expectedPod:  "pod1",
			expectedJSON: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod, json, err := parsePsArgs(tt.args)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedPod, pod)
				assert.Equal(t, tt.expectedJSON, json)
			}
		})
	}
}

func TestInspectArgumentParsing(t *testing.T) {
	tests := []struct {
		name              string
		args              []string
		expectError       bool
		expectedContainer string
		expectedJSON      bool
	}{
		{
			name:        "missing json output flag",
			args:        []string{"container1"},
			expectError: true,
		},
		{
			name:        "missing container ID",
			args:        []string{"-o", "json"},
			expectError: true,
		},
		{
			name:              "valid args",
			args:              []string{"-o", "json", "container1"},
			expectError:       false,
			expectedContainer: "container1",
			expectedJSON:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container, json, err := parseInspectArgs(tt.args)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedContainer, container)
				assert.Equal(t, tt.expectedJSON, json)
			}
		})
	}
}
