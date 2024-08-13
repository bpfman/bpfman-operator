//go:build integration_tests
// +build integration_tests

// Package loadimagearchive extends the functionality of kind clusters
// to load OCI images using either Podman or Docker. Unlike the
// standard kind command which is limited to Docker through `kind load
// docker-image`, this add-on supports both systems by saving images
// as tarballs and then using the `kind load image-archive` command.
package loadimagearchive

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
)

// loadIntoKind orchestrates the loading of OCI images into a Kind
// cluster by creating temporary tarballs and loading them.
func (a *Addon) loadIntoKind(ctx context.Context, cluster clusters.Cluster) error {
	if len(a.images) == 0 {
		return fmt.Errorf("no images provided")
	}

	tmpDir, err := os.MkdirTemp("", "kind-image-")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	for i, image := range a.images {
		tarballPath := filepath.Join(tmpDir, fmt.Sprintf("image%d.tar", i+1))
		if err := saveImage(a.ociBin, image, tarballPath); err != nil {
			return err
		}
		if err := loadImageToKind(ctx, cluster, tarballPath); err != nil {
			return err
		}
		a.loaded = true
	}

	return nil
}

// loadImageToKind loads a single image tarball into the Kind cluster.
func loadImageToKind(ctx context.Context, cluster clusters.Cluster, tarballPath string) error {
	loadArgs := []string{"load", "image-archive", "--name", cluster.Name(), tarballPath}
	cmd := exec.CommandContext(ctx, "kind", loadArgs...)
	stderr := &bytes.Buffer{}
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", stderr.String(), err)
	}

	return nil
}

// saveImage saves a specified OCI image as a tarball at the given
// path.
func saveImage(ociBin, imageName, tarballPath string) error {
	saveCmd := exec.Command(ociBin, "save", "-o", tarballPath, imageName)
	stderr := &bytes.Buffer{}
	saveCmd.Stdout = io.Discard
	saveCmd.Stderr = stderr

	if err := saveCmd.Run(); err != nil {
		return fmt.Errorf("'%v save -o %v %v': %s: %w", ociBin, tarballPath, imageName, stderr.String(), err)
	}

	return nil
}
