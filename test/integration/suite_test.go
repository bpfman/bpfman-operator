//go:build integration_tests
// +build integration_tests

package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/types/kind"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

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
)

const (
	bpfmanKustomize = "../../config/test"
	bpfmanConfigMap = "../../config/bpfman-deployment/config.yaml"
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

	// deploy the BPFMAN Operator and relevant CRDs.
	if !skipBpfmanDeploy {
		fmt.Println("INFO: deploying bpfman operator to test cluster")
		exitOnErr(clusters.KustomizeDeployForCluster(ctx, env.Cluster(), bpfmanKustomize))
		if !keepKustomizeDeploys {
			addCleanup(func(context.Context) error {
				cleanupLog("delete bpfman configmap to cleanup bpfman daemon")
				env.Cluster().Client().CoreV1().ConfigMaps(internal.BpfmanNamespace).Delete(ctx, internal.BpfmanConfigName, metav1.DeleteOptions{})
				clusters.DeleteManifestByYAML(ctx, env.Cluster(), bpfmanConfigMap)
				waitForBpfmanConfigDelete(ctx, env)
				cleanupLog("deleting bpfman namespace")
				return env.Cluster().Client().CoreV1().Namespaces().Delete(ctx, internal.BpfmanNamespace, metav1.DeleteOptions{})
			})
		}
	} else {
		fmt.Println("INFO: skipping bpfman deployment (SKIP_BPFMAN_DEPLOY=true)")
	}

	bpfmanClient = bpfmanHelpers.GetClientOrDie()
	exitOnErr(waitForBpfmanReadiness(ctx, env))

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

			_, err := env.Cluster().Client().CoreV1().ConfigMaps(internal.BpfmanNamespace).Get(ctx, internal.BpfmanConfigName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					fmt.Println("INFO: bpfman configmap deleted successfully")
					return nil
				}
				return err
			}
		}
	}
}
