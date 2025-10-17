//go:build integration_tests
// +build integration_tests

package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/types/kind"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/bpfman/bpfman-operator/apis/v1alpha1"
	"github.com/bpfman/bpfman-operator/internal"
	"github.com/bpfman/bpfman-operator/pkg/client/clientset"
	bpfmanHelpers "github.com/bpfman/bpfman-operator/pkg/helpers"

	"github.com/bpfman/bpfman-operator/test/integration/loadimagearchive"
)

var (
	ctx          context.Context
	cancel       context.CancelFunc
	env          environments.Environment
	bpfmanClient *clientset.Clientset

	// These images should already be built on the node so they can
	// be loaded into kind.
	bpfmanAgentImage    = os.Getenv("BPFMAN_AGENT_IMG")
	bpfmanOperatorImage = os.Getenv("BPFMAN_OPERATOR_IMG")

	existingCluster      = os.Getenv("USE_EXISTING_KIND_CLUSTER")
	keepTestCluster      = func() bool { return os.Getenv("TEST_KEEP_CLUSTER") == "true" || existingCluster != "" }()
	keepKustomizeDeploys = func() bool { return os.Getenv("TEST_KEEP_KUSTOMIZE_DEPLOYS") == "true" }()
	skipBpfmanDeploy     = func() bool { return os.Getenv("SKIP_BPFMAN_DEPLOY") == "true" }()

	cleanup = []func(context.Context) error{}

	bpfmanConfig = v1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name: internal.BpfmanConfigName,
		},
		Spec: v1alpha1.ConfigSpec{
			Namespace: "bpfman",
			Image:     "quay.io/bpfman/bpfman:latest",
			LogLevel:  "bpfman=debug",
			Configuration: `[database]
max_retries = 30
millisec_delay = 10000
[signing]
allow_unsigned = true
verify_enabled = true`,
			Agent: v1alpha1.AgentSpec{
				Image:           "CHANGEME",
				LogLevel:        "info",
				HealthProbePort: 8175,
			},
		},
	}
)

const (
	bpfmanCRD              = "../../config/crd"
	bpfmanKustomize        = "../../config/test"
	bpfmanKustomizationEnv = "kustomization.yaml.env"
	bpfmanConfigMap        = "../../config/bpfman-deployment/config.yaml"
	newImageName           = "NEW_IMAGE_NAME"
	newImageTag            = "NEW_IMAGE_TAG"
)

func TestMain(m *testing.M) {
	logf.SetLogger(zap.New())

	ociBin := os.Getenv("OCI_BIN")
	if ociBin == "" {
		ociBin = "docker" // default if OCI_BIN is not set.
	}

	// check that we have the bpfman-agent, and bpfman-operator images to use for the tests.
	// generally the runner of the tests should have built these from the latest
	// changes prior to the tests and fed them to the test suite.
	if bpfmanAgentImage == "" || bpfmanOperatorImage == "" {
		exitOnErr(fmt.Errorf("BPFMAN_AGENT_IMG, and BPFMAN_OPERATOR_IMG must be provided"))
	} else {
		fmt.Printf("INFO: using bpfmanAgentImage=%s and bpfmanOperatorImage=%s\n", bpfmanAgentImage, bpfmanOperatorImage)
	}

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	// to use the provided bpfman-agent, and bpfman-operator images we will need to add
	// them as images to load in the test cluster via an addon.
	fmt.Println("INFO: Loading images")
	loadImages, err := loadimagearchive.NewBuilder(ociBin).WithImage(bpfmanAgentImage)
	exitOnErr(err)
	loadImages, err = loadImages.WithImage(bpfmanOperatorImage)
	exitOnErr(err)

	if existingCluster != "" {
		fmt.Printf("INFO: existing kind cluster %s was provided\n", existingCluster)

		// if an existing cluster was provided, build a test env out of that instead
		cluster, err := kind.NewFromExisting(existingCluster)
		exitOnErr(err)
		env, err = environments.NewBuilder().WithAddons(loadImages.Build()).WithExistingCluster(cluster).Build(ctx)
		exitOnErr(err)
	} else {
		fmt.Println("INFO: creating a new kind cluster")
		// create the testing environment and cluster
		env, err = environments.NewBuilder().WithAddons(loadImages.Build()).Build(ctx)
		exitOnErr(err)

		fmt.Printf("INFO: new kind cluster %s was created\n", env.Cluster().Name())
	}

	if !keepTestCluster {
		addCleanup(func(context.Context) error {
			cleanupLog("cleaning up test environment and cluster %s\n", env.Cluster().Name())
			return env.Cleanup(ctx)
		})
	}

	fmt.Println("INFO: Get the bpfman client")
	bpfmanClient = bpfmanHelpers.GetClientOrDie()

	// deploy the BPFMAN Operator and relevant CRDs.
	if !skipBpfmanDeploy {
		// Install the CRDs, RBAC and operator-deployment first.
		fmt.Println("INFO: deploying bpfman operator to test cluster")
		exitOnErr(generateKustomization(bpfmanKustomize, bpfmanKustomizationEnv, bpfmanOperatorImage))
		exitOnErr(clusters.KustomizeDeployForCluster(ctx, env.Cluster(), bpfmanKustomize))
		// Then, deploy the Config resource.
		bpfmanConfig.Spec.Agent.Image = bpfmanAgentImage
		_, err := bpfmanClient.BpfmanV1alpha1().Configs().Create(ctx, &bpfmanConfig, metav1.CreateOptions{})
		exitOnErr(err)
		if !keepKustomizeDeploys {
			addCleanup(func(context.Context) error {
				ctxTimeout, cancelFunc := context.WithTimeout(ctx, 5*time.Minute)
				defer cancelFunc()

				cleanupLog("delete bpfman Config to cleanup bpfman daemon")
				bpfmanClient.BpfmanV1alpha1().Configs().Delete(ctxTimeout, internal.BpfmanConfigName, metav1.DeleteOptions{})
				waitForBpfmanConfigDelete(ctxTimeout, env)
				cleanupLog("deleting bpfman namespace")
				return env.Cluster().Client().CoreV1().Namespaces().Delete(ctxTimeout, internal.BpfmanNamespace, metav1.DeleteOptions{})
			})
		}
	} else {
		fmt.Println("INFO: skipping bpfman deployment (SKIP_BPFMAN_DEPLOY=true)")
	}

	// Make sure bpfman resources are up.
	ctxTimeout, cancelFunc := context.WithTimeout(ctx, 5*time.Minute)
	defer cancelFunc()
	exitOnErr(waitForBpfmanReadiness(ctxTimeout, env))

	exit := m.Run()
	// If there's any errors in e2e tests dump diagnostics
	if exit != 0 {
		_, err := env.Cluster().DumpDiagnostics(ctx, "bpfman-e2e-test")
		exitOnErr(err)
	}

	exitOnErr(runCleanup())

	os.Exit(exit)
}

func exitOnErr(err error) {
	if err == nil {
		return
	}

	if cleanupErr := runCleanup(); cleanupErr != nil {
		err = fmt.Errorf("%s; %w", err, cleanupErr)
	}

	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func addCleanup(job func(context.Context) error) {
	// prepend so that cleanup runs in reverse order
	cleanup = append([]func(context.Context) error{job}, cleanup...)
}

func cleanupLog(msg string, args ...any) {
	fmt.Printf(fmt.Sprintf("INFO: %s\n", msg), args...)
}

func runCleanup() (cleanupErr error) {
	if len(cleanup) < 1 {
		return
	}

	fmt.Println("INFO: running cleanup jobs")
	for _, job := range cleanup {
		if err := job(ctx); err != nil {
			cleanupErr = fmt.Errorf("%s; %w", err, cleanupErr)
		}
	}
	cleanup = nil
	return
}

func waitForBpfmanReadiness(ctx context.Context, env environments.Environment) error {
	for {
		time.Sleep(2 * time.Second)
		select {
		case <-ctx.Done():
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("context completed while waiting for components: %w", err)
			}
			return fmt.Errorf("context completed while waiting for components")
		default:
			fmt.Println("INFO: waiting for bpfman")
			var controlplaneReady, dataplaneReady bool

			controlplane, err := env.Cluster().Client().AppsV1().Deployments(internal.BpfmanNamespace).Get(ctx, internal.BpfmanOperatorName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					fmt.Println("INFO: bpfman-operator dep not found yet")
					continue
				}
				return err
			}
			if controlplane.Status.AvailableReplicas > 0 {
				controlplaneReady = true
			}

			dataplane, err := env.Cluster().Client().AppsV1().DaemonSets(internal.BpfmanNamespace).Get(ctx, internal.BpfmanDsName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					fmt.Println("INFO: bpfman daemon not found yet")
					continue
				}
				return err
			}
			if dataplane.Status.NumberAvailable > 0 {
				dataplaneReady = true
			}

			if controlplaneReady && dataplaneReady {
				fmt.Println("INFO: bpfman-operator is ready")
				return nil
			}
		}
	}
}

func waitForBpfmanConfigDelete(ctx context.Context, env environments.Environment) error {
	for {
		time.Sleep(2 * time.Second)
		select {
		case <-ctx.Done():
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("context completed while waiting for components: %w", err)
			}
			return fmt.Errorf("context completed while waiting for components")
		default:
			fmt.Println("INFO: waiting for bpfman config deletion")

			checks := []struct {
				check func() error
				msg   string
			}{
				{
					check: func() error {
						_, err := env.Cluster().Client().StorageV1().CSIDrivers().Get(ctx,
							internal.BpfmanCsiDriverName, metav1.GetOptions{})
						return err
					},
					msg: "INFO: bpfman csidriver deleted successfully",
				},
				{
					check: func() error {
						_, err := env.Cluster().Client().CoreV1().ConfigMaps(internal.BpfmanNamespace).Get(ctx,
							internal.BpfmanConfigName, metav1.GetOptions{})
						return err
					},
					msg: "INFO: bpfman configmap deleted successfully",
				},
				{
					check: func() error {
						_, err := env.Cluster().Client().AppsV1().DaemonSets(internal.BpfmanNamespace).Get(ctx,
							internal.BpfmanDsName, metav1.GetOptions{})
						return err
					},
					msg: "INFO: bpfman daemon daemonset deleted successfully",
				},
				{
					check: func() error {
						_, err := env.Cluster().Client().AppsV1().DaemonSets(internal.BpfmanNamespace).Get(ctx,
							internal.BpfmanMetricsProxyDsName, metav1.GetOptions{})
						return err
					},
					msg: "INFO: bpfman metrics proxy daemonset deleted successfully",
				},
			}

			deleteCount := 0
			for _, c := range checks {
				if err := c.check(); err != nil {
					if !errors.IsNotFound(err) {
						return err
					}
					fmt.Println(c.msg)
					deleteCount++
				}
			}
			if deleteCount == len(checks) {
				return nil
			}
		}
	}
}

func generateKustomization(kustomizeDir, templateFile, operatorImage string) error {
	// Check if image contains SHA digest
	if strings.Contains(operatorImage, "@sha256:") {
		return fmt.Errorf("image with SHA digest not supported: %s", operatorImage)
	}

	// Read kustomization.yaml.env template and replace placeholders
	envData, err := os.ReadFile(filepath.Join(kustomizeDir, templateFile))
	if err != nil {
		return err
	}

	// Split operatorImage into name and tag
	imageParts := strings.Split(operatorImage, ":")
	imageName := imageParts[0]
	imageTag := "latest"
	if len(imageParts) > 1 {
		imageTag = imageParts[1]
	}

	// Replace placeholders
	kustomizationContent := string(envData)
	kustomizationContent = strings.ReplaceAll(kustomizationContent, newImageName, imageName)
	kustomizationContent = strings.ReplaceAll(kustomizationContent, newImageTag, imageTag)

	// Write to kustomization.yaml
	return os.WriteFile(filepath.Join(kustomizeDir, "kustomization.yaml"), []byte(kustomizationContent), 0644)
}
