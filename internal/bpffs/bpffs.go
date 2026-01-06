/*
Copyright 2026.

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

// Package bpffs provides functions to check and mount the BPF
// filesystem.
package bpffs

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"
)

const (
	// DefaultMountPoint is the standard location for the BPF
	// filesystem.
	DefaultMountPoint = "/sys/fs/bpf"

	// DefaultMountInfoPath is the path to the mountinfo file.
	DefaultMountInfoPath = "/proc/self/mountinfo"

	// defaultScanMaxLineLen is the maximum line length for
	// scanning mountinfo. Some nodes/runtimes can produce long
	// lines; this prevents ErrTooLong.
	defaultScanMaxLineLen = 1024 * 1024
)

// IsMounted reports whether a bpffs is mounted at mountPoint by
// parsing mountInfoPath (e.g. /proc/self/mountinfo).
//
// The mountinfo format is documented in proc(5). Each line contains:
//
//	mount_id parent_id major:minor root mount_point options [optional_fields...] - fstype source super_options
//
// Example bpffs entry:
//
//	30 22 0:27 / /sys/fs/bpf rw,nosuid shared:9 - bpf bpf rw,mode=700
//	              ↑                               ↑
//	              mount_point (fields[4])         fstype (after " - ")
//
// The key insight from libmount (util-linux) is that the separator "
// - " must be found using string search, not by assuming a fixed
// field position. This is because optional fields (like "shared:N"
// for mount propagation) may be present between the mount options and
// the separator.
func IsMounted(mountInfoPath, mountPoint string) (bool, error) {
	file, err := os.Open(mountInfoPath)
	if err != nil {
		return false, fmt.Errorf("opening mountinfo: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), defaultScanMaxLineLen)

	for scanner.Scan() {
		line := scanner.Text()

		// Find the separator " - " which precedes "fstype
		// source super_options". This is how libmount parses
		// mountinfo (see mnt_parse_mountinfo_line).
		sepIdx := strings.Index(line, " - ")
		if sepIdx == -1 {
			continue
		}

		// Parse the prefix: mount_id parent_id major:minor
		// root mount_point ...
		prefix := line[:sepIdx]
		fields := strings.Fields(prefix)
		if len(fields) < 5 {
			continue
		}
		mntPoint := fields[4]

		// Parse the suffix after " - ": fstype source
		// super_options.
		suffix := line[sepIdx+3:] // skip " - "
		suffixFields := strings.Fields(suffix)
		if len(suffixFields) < 1 {
			continue
		}
		fsType := suffixFields[0]

		// Match: bpffs at the requested path.
		if mntPoint == mountPoint && fsType == "bpf" {
			return true, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("reading mountinfo: %w", err)
	}

	return false, nil
}

// Mount mounts a bpffs at mountPoint, creating the directory if needed.
func Mount(mountPoint string) error {
	fi, err := os.Stat(mountPoint)
	switch {
	case err == nil:
		if !fi.IsDir() {
			return fmt.Errorf("mount point exists but is not a directory")
		}
	case os.IsNotExist(err):
		if err := os.MkdirAll(mountPoint, 0755); err != nil {
			return fmt.Errorf("creating mount point directory: %w", err)
		}
	default:
		return fmt.Errorf("stat mount point: %w", err)
	}

	if err := syscall.Mount("bpffs", mountPoint, "bpf", 0, ""); err != nil {
		return fmt.Errorf("mount syscall: %w", err)
	}

	return nil
}

// Unmount unmounts the bpffs at mountPoint.
func Unmount(mountPoint string) error {
	if err := syscall.Unmount(mountPoint, 0); err != nil {
		return fmt.Errorf("unmount syscall: %w", err)
	}
	return nil
}

// EnsureMounted ensures a bpffs is mounted at mountPoint. It checks
// mountInfoPath (e.g. /proc/self/mountinfo) for an existing bpf mount
// at mountPoint; if none is found, it mounts one.
//
// Equivalent to:
//
//	if ! findmnt --noheadings --types bpf <mountPoint>; then
//	  mount bpffs <mountPoint> -t bpf
//	fi
func EnsureMounted(mountInfoPath, mountPoint string) error {
	mounted, err := IsMounted(mountInfoPath, mountPoint)
	if err != nil {
		return err
	}
	if mounted {
		return nil
	}
	return Mount(mountPoint)
}
