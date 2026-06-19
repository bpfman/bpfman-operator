//go:build integration_tests
// +build integration_tests

package integration

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/blang/semver/v4"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
)

// existingClusterType marks a cluster that the suite attached to
// rather than provisioned, and therefore must never tear down.
const existingClusterType clusters.Type = "existing"

// existingCluster is a clusters.Cluster backed by whatever cluster the
// caller's kubeconfig already points at (KUBECONFIG plus current
// context). Unlike the kind-backed path it issues no kind or container
// commands, so it works against any reachable cluster -- in particular
// an OpenShift cluster reached via `oc login`.
type existingCluster struct {
	name   string
	client *kubernetes.Clientset
	cfg    *rest.Config
	addons clusters.Addons
}

// newExistingCluster builds a cluster from the ambient kubeconfig,
// using the same loading rules as the bpfman client so that both
// resolve to the same current context.
func newExistingCluster() (*existingCluster, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})

	raw, err := cc.RawConfig()
	if err != nil {
		return nil, fmt.Errorf("reading kubeconfig: %w", err)
	}

	cfg, err := cc.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("building rest config: %w", err)
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("building clientset: %w", err)
	}

	return &existingCluster{
		name:   raw.CurrentContext,
		client: client,
		cfg:    cfg,
		addons: make(clusters.Addons),
	}, nil
}

func (c *existingCluster) Name() string {
	return c.name
}

func (c *existingCluster) Type() clusters.Type {
	return existingClusterType
}

func (c *existingCluster) Version() (semver.Version, error) {
	versionInfo, err := c.client.ServerVersion()
	if err != nil {
		return semver.Version{}, err
	}
	return semver.Parse(strings.TrimPrefix(versionInfo.String(), "v"))
}

func (c *existingCluster) Client() *kubernetes.Clientset {
	return c.client
}

func (c *existingCluster) Config() *rest.Config {
	return c.cfg
}

// Cleanup is a no-op: the suite did not create this cluster, so it has
// no business deleting it.
func (c *existingCluster) Cleanup(context.Context) error {
	return nil
}

func (c *existingCluster) GetAddon(name clusters.AddonName) (clusters.Addon, error) {
	addon, ok := c.addons[name]
	if !ok {
		return nil, fmt.Errorf("addon %s not found", name)
	}
	return addon, nil
}

func (c *existingCluster) ListAddons() []clusters.Addon {
	addonList := make([]clusters.Addon, 0, len(c.addons))
	for _, addon := range c.addons {
		addonList = append(addonList, addon)
	}
	return addonList
}

func (c *existingCluster) DeployAddon(ctx context.Context, addon clusters.Addon) error {
	if _, ok := c.addons[addon.Name()]; ok {
		return fmt.Errorf("addon component %s is already loaded into cluster %s", addon.Name(), c.name)
	}
	c.addons[addon.Name()] = addon
	return addon.Deploy(ctx, c)
}

func (c *existingCluster) DeleteAddon(ctx context.Context, addon clusters.Addon) error {
	if _, ok := c.addons[addon.Name()]; !ok {
		return nil
	}
	if err := addon.Delete(ctx, c); err != nil {
		return err
	}
	delete(c.addons, addon.Name())
	return nil
}

func (c *existingCluster) DumpDiagnostics(ctx context.Context, meta string) (string, error) {
	outDir, err := os.MkdirTemp(os.TempDir(), clusters.DiagnosticOutDirectoryPrefix)
	if err != nil {
		return "", err
	}
	return outDir, clusters.DumpDiagnostics(ctx, c, meta, outDir)
}

func (c *existingCluster) IPFamily() clusters.IPFamily {
	return clusters.IPv4
}
