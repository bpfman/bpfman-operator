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

// Package main implements bpfman-crictl, a minimal crictl replacement
// for bpfman-operator.
//
// This is a cut-down version of crictl that implements only the
// specific functionality needed by bpfman-operator for container PID
// discovery:
//
//   - pods --name <name> -o json     (list pods by exact name)
//   - ps --pod <pod-id> -o json      (list containers in a pod)
//   - inspect -o json <container-id> (inspect container for PID)
//
// Unlike the full crictl implementation, this version:
//
//   - Uses exact string matching for pod names instead of regex matching
//   - Only supports JSON output format
//   - Implements timeout-based socket discovery matching crictl's behavior
//   - Provides a native Go CRI gRPC client instead of external process calls
//
// This eliminates the external crictl dependency while maintaining
// functionality for bpfman-operator's container discovery use case.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/bpfman/bpfman-operator/pkg/crictl"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <command> [args...]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  pods --name <name> -o json     List pods\n")
		fmt.Fprintf(os.Stderr, "  ps --pod <pod-id> -o json      List containers\n")
		fmt.Fprintf(os.Stderr, "  inspect -o json <container-id> Inspect container\n")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := crictl.NewClient(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating CRI client: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	command := os.Args[1]
	args := os.Args[2:]

	var output string
	switch command {
	case "pods":
		output, err = handlePodsCommand(client, args)
	case "ps":
		output, err = handlePsCommand(client, args)
	case "inspect":
		output, err = handleInspectCommand(client, args)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(output)
}

func handlePodsCommand(client *crictl.Client, args []string) (string, error) {
	var nameFilter string
	var outputJSON bool

	// Parse arguments.
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--name":
			if i+1 < len(args) {
				nameFilter = args[i+1]
				i++ // Skip next arg
			}
		case "-o":
			if i+1 < len(args) && args[i+1] == "json" {
				outputJSON = true
				i++ // Skip next arg
			}
		}
	}

	if !outputJSON {
		return "", fmt.Errorf("only JSON output is supported")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.ListPods(ctx, nameFilter)
	if err != nil {
		return "", fmt.Errorf("listing pods: %w", err)
	}

	jsonOutput, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling JSON: %w", err)
	}

	return string(jsonOutput), nil
}

func handlePsCommand(client *crictl.Client, args []string) (string, error) {
	var podID string
	var outputJSON bool

	// Parse arguments.
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--pod":
			if i+1 < len(args) {
				podID = args[i+1]
				i++ // Skip next arg
			}
		case "-o":
			if i+1 < len(args) && args[i+1] == "json" {
				outputJSON = true
				i++ // Skip next arg
			}
		}
	}

	if !outputJSON {
		return "", fmt.Errorf("only JSON output is supported")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.ListContainers(ctx, podID)
	if err != nil {
		return "", fmt.Errorf("listing containers: %w", err)
	}

	jsonOutput, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling JSON: %w", err)
	}

	return string(jsonOutput), nil
}

func handleInspectCommand(client *crictl.Client, args []string) (string, error) {
	var containerID string
	var outputJSON bool

	// Parse arguments.
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o":
			if i+1 < len(args) && args[i+1] == "json" {
				outputJSON = true
				i++ // Skip next arg
			}
		default:
			// Assume it's the container ID if it doesn't start with -
			if args[i][0] != '-' {
				containerID = args[i]
			}
		}
	}

	if !outputJSON {
		return "", fmt.Errorf("only JSON output is supported")
	}

	if containerID == "" {
		return "", fmt.Errorf("container ID is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.InspectContainer(ctx, containerID)
	if err != nil {
		return "", fmt.Errorf("inspecting container: %w", err)
	}

	jsonOutput, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling JSON: %w", err)
	}

	return string(jsonOutput), nil
}
