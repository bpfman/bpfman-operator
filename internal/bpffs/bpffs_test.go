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

package bpffs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bpfman/bpfman-operator/internal/bpffs"
)

func TestBPFFSIsMounted(t *testing.T) {
	tests := []struct {
		name       string
		mountinfo  string
		mountPoint string
		want       bool
	}{
		{
			name: "util-linux format without propagation - no bpf",
			mountinfo: `15 20 0:3 / /proc rw,relatime - proc /proc rw
16 20 0:15 / /sys rw,relatime - sysfs /sys rw
17 20 0:5 / /dev rw,relatime - devtmpfs udev rw,size=1983516k,nr_inodes=495879,mode=755
20 1 8:4 / / rw,noatime - ext3 /dev/sda4 rw,errors=continue,user_xattr,acl,barrier=0,data=ordered
`,
			mountPoint: "/sys/fs/bpf",
			want:       false,
		},
		{
			name: "util-linux format without propagation - with bpf",
			mountinfo: `15 20 0:3 / /proc rw,relatime - proc /proc rw
16 20 0:15 / /sys rw,relatime - sysfs /sys rw
17 20 0:5 / /dev rw,relatime - devtmpfs udev rw,size=1983516k,nr_inodes=495879,mode=755
20 1 8:4 / / rw,noatime - ext3 /dev/sda4 rw,errors=continue,user_xattr,acl,barrier=0,data=ordered
48 16 0:39 / /sys/fs/bpf rw,nosuid,nodev,noexec,relatime - bpf bpf rw,mode=700
`,
			mountPoint: "/sys/fs/bpf",
			want:       true,
		},
		{
			name: "NixOS format with propagation - no bpf",
			mountinfo: `22 31 0:6 / /dev rw,nosuid shared:12 - devtmpfs devtmpfs rw,size=6532720k,nr_inodes=16327128,mode=755
25 31 0:23 / /proc rw,nosuid,nodev,noexec,relatime shared:5 - proc proc rw
28 31 0:26 / /sys rw,nosuid,nodev,noexec,relatime shared:6 - sysfs sysfs rw
36 28 0:35 / /sys/fs/cgroup rw,nosuid,nodev,noexec,relatime shared:8 - cgroup2 cgroup2 rw,nsdelegate,memory_recursiveprot
`,
			mountPoint: "/sys/fs/bpf",
			want:       false,
		},
		{
			name: "NixOS format with propagation - with bpf",
			mountinfo: `22 31 0:6 / /dev rw,nosuid shared:12 - devtmpfs devtmpfs rw,size=6532720k,nr_inodes=16327128,mode=755
25 31 0:23 / /proc rw,nosuid,nodev,noexec,relatime shared:5 - proc proc rw
28 31 0:26 / /sys rw,nosuid,nodev,noexec,relatime shared:6 - sysfs sysfs rw
36 28 0:35 / /sys/fs/cgroup rw,nosuid,nodev,noexec,relatime shared:8 - cgroup2 cgroup2 rw,nsdelegate,memory_recursiveprot
39 28 0:38 / /sys/fs/bpf rw,nosuid,nodev,noexec,relatime shared:11 - bpf bpf rw,gid=983,mode=770
`,
			mountPoint: "/sys/fs/bpf",
			want:       true,
		},
		{
			name: "CoreOS format with propagation - no bpf",
			mountinfo: `21 72 0:20 / /proc rw,nosuid,nodev,noexec,relatime shared:15 - proc proc rw
22 72 0:21 / /sys rw,nosuid,nodev,noexec,relatime shared:5 - sysfs sysfs rw,seclabel
23 72 0:5 / /dev rw,nosuid shared:11 - devtmpfs devtmpfs rw,seclabel,size=4096k,nr_inodes=4094014,mode=755,inode64
28 22 0:25 / /sys/fs/cgroup rw,nosuid,nodev,noexec,relatime shared:7 - cgroup2 cgroup2 rw,seclabel
`,
			mountPoint: "/sys/fs/bpf",
			want:       false,
		},
		{
			name: "CoreOS format with propagation - with bpf",
			mountinfo: `21 72 0:20 / /proc rw,nosuid,nodev,noexec,relatime shared:15 - proc proc rw
22 72 0:21 / /sys rw,nosuid,nodev,noexec,relatime shared:5 - sysfs sysfs rw,seclabel
23 72 0:5 / /dev rw,nosuid shared:11 - devtmpfs devtmpfs rw,seclabel,size=4096k,nr_inodes=4094014,mode=755,inode64
28 22 0:25 / /sys/fs/cgroup rw,nosuid,nodev,noexec,relatime shared:7 - cgroup2 cgroup2 rw,seclabel
30 22 0:27 / /sys/fs/bpf rw,nosuid,nodev,noexec,relatime shared:9 - bpf bpf rw,mode=700
`,
			mountPoint: "/sys/fs/bpf",
			want:       true,
		},
		{
			name: "different mount point",
			mountinfo: `30 22 0:27 / /sys/fs/bpf rw,nosuid,nodev,noexec,relatime shared:9 - bpf bpf rw,mode=700
`,
			mountPoint: "/some/other/path",
			want:       false,
		},
		{
			name: "multiple optional fields",
			mountinfo: `30 22 0:27 / /sys/fs/bpf rw,nosuid shared:9 master:1 - bpf bpf rw,mode=700
`,
			mountPoint: "/sys/fs/bpf",
			want:       true,
		},
		{
			name:       "empty file",
			mountinfo:  "",
			mountPoint: "/sys/fs/bpf",
			want:       false,
		},
		{
			name: "malformed line without separator",
			mountinfo: `this line has no separator
30 22 0:27 / /sys/fs/bpf rw,nosuid shared:9 - bpf bpf rw,mode=700
`,
			mountPoint: "/sys/fs/bpf",
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary file with the test mountinfo content
			tmpDir := t.TempDir()
			mountInfoPath := filepath.Join(tmpDir, "mountinfo")
			if err := os.WriteFile(mountInfoPath, []byte(tt.mountinfo), 0644); err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			got, err := bpffs.IsMounted(mountInfoPath, tt.mountPoint)
			if err != nil {
				t.Fatalf("IsMounted() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("IsMounted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBPFFSIsMounted_FileNotFound(t *testing.T) {
	_, err := bpffs.IsMounted("/nonexistent/path/mountinfo", "/sys/fs/bpf")
	if err == nil {
		t.Error("IsMounted() expected error for nonexistent file, got nil")
	}
}

func TestBPFFSIsMounted_LongLine(t *testing.T) {
	// Generate a mountinfo line > 64 KiB (default scanner limit).
	// This tests the scanner buffer increase (prevents ErrTooLong).
	// Target ~70 KiB to ensure it fails without the buffer bump.
	var b strings.Builder
	b.WriteString("30 22 0:27 / /sys/fs/bpf rw")
	for b.Len() < 70*1024 {
		b.WriteString(",option")
		b.WriteByte(byte('a' + (b.Len() % 26)))
	}
	b.WriteString(" shared:9 - bpf bpf rw,mode=700\n")
	mountinfo := b.String()

	tmpDir := t.TempDir()
	mountInfoPath := filepath.Join(tmpDir, "mountinfo")
	if err := os.WriteFile(mountInfoPath, []byte(mountinfo), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	got, err := bpffs.IsMounted(mountInfoPath, "/sys/fs/bpf")
	if err != nil {
		t.Fatalf("IsMounted() error = %v (scanner buffer may be too small)", err)
	}
	if !got {
		t.Error("IsMounted() = false, want true")
	}
}
