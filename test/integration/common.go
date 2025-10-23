//go:build integration_tests
// +build integration_tests

package integration

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/bpfman/bpfman-operator/apis/v1alpha1"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	bpfmanNamespace      = "bpfman"
	bpfmanContainer      = "bpfman"
	bpfmanDaemonSelector = "name=bpfman-daemon"
)

func doKprobeCheck(t *testing.T, output *bytes.Buffer) bool {
	str := `Kprobe count: ([0-9]+)`
	if ok, count := doProbeCommonCheck(t, output, str); ok {
		t.Logf("counted %d kprobe executions so far, BPF program is functioning", count)
		return true
	}
	return false
}

func doAppKprobeCheck(t *testing.T, output *bytes.Buffer) bool {
	str := `Kprobe: count: ([0-9]+)`
	if ok, count := doProbeCommonCheck(t, output, str); ok {
		t.Logf("counted %d kprobe executions so far, BPF program is functioning", count)
		return true
	}
	return false
}

func doTcCheck(t *testing.T, output *bytes.Buffer) bool {
	if strings.Contains(output.String(), "packets received") && strings.Contains(output.String(), "bytes received") {
		t.Log("TC BPF program is functioning")
		return true
	}
	return false
}

func doAppTcCheck(t *testing.T, output *bytes.Buffer) bool {
	str := `TC: received (\d+) packets`
	if ok, count := doProbeCommonCheck(t, output, str); ok {
		t.Logf("TC BPF program is functioning packets: %d", count)
		return true
	}
	return false
}

func doTcxCheck(t *testing.T, output *bytes.Buffer) bool {
	if strings.Contains(output.String(), "packets received") && strings.Contains(output.String(), "bytes received") {
		t.Log("TCX BPF program is functioning")
		return true
	}
	return false
}

func doAppTcxCheck(t *testing.T, output *bytes.Buffer) bool {
	str := `TCX: received (\d+) packets`
	if ok, count := doProbeCommonCheck(t, output, str); ok {
		t.Logf("TCX BPF program is functioning packets: %d", count)
		return true
	}
	return false
}

func doTracepointCheck(t *testing.T, output *bytes.Buffer) bool {
	str := `SIGUSR1 signal count: ([0-9]+)`
	if ok, count := doProbeCommonCheck(t, output, str); ok {
		t.Logf("counted %d SIGUSR1 signals so far, BPF program is functioning", count)
		return true
	}
	return false
}

func doAppTracepointCheck(t *testing.T, output *bytes.Buffer) bool {
	str := `Tracepoint: SIGUSR1 signal count: ([0-9]+)`
	if ok, count := doProbeCommonCheck(t, output, str); ok {
		t.Logf("counted %d SIGUSR1 signals so far, BPF program is functioning", count)
		return true
	}
	return false
}

func doUprobeCheck(t *testing.T, output *bytes.Buffer) bool {
	str := `Uprobe count: ([0-9]+)`
	if ok, count := doProbeCommonCheck(t, output, str); ok {
		t.Logf("counted %d uprobe executions so far, BPF program is functioning", count)
		return true
	}
	return false
}

func doAppUprobeCheck(t *testing.T, output *bytes.Buffer) bool {
	str := `Uprobe: count: ([0-9]+)`
	if ok, count := doProbeCommonCheck(t, output, str); ok {
		t.Logf("counted %d uprobe executions so far, BPF program is functioning", count)
		return true
	}
	return false
}

func doXdpCheck(t *testing.T, output *bytes.Buffer) bool {
	if strings.Contains(output.String(), "packets received") && strings.Contains(output.String(), "bytes received") {
		t.Log("XDP BPF program is functioning")
		return true
	}
	return false
}

func doAppXdpCheck(t *testing.T, output *bytes.Buffer) bool {
	str := `XDP: received (\d+) packets`
	if ok, count := doProbeCommonCheck(t, output, str); ok {
		t.Logf("XDP BPF program is functioning packets: %d", count)
		return true
	}
	return false
}

func doProbeCommonCheck(t *testing.T, output *bytes.Buffer, str string) (bool, int) {
	want := regexp.MustCompile(str)
	matches := want.FindAllStringSubmatch(output.String(), -1)
	numMatches := len(matches)
	if numMatches >= 1 && len(matches[numMatches-1]) >= 2 {
		count, err := strconv.Atoi(matches[numMatches-1][1])
		require.NoError(t, err)
		if count > 0 {
			return true, count
		}
	}
	return false, 0
}

// clusterBpfApplicationStateSuccess returns a function that checks if the expected number of
// ClusterBpfApplications matching the label selector have reached a successful state.
func clusterBpfApplicationStateSuccess(t *testing.T, labelSelector string, numExpected int) func() bool {
	return func() bool {
		// Fetch all ClusterBpfApplications matching the label selector.
		apps, err := bpfmanClient.BpfmanV1alpha1().ClusterBpfApplications().List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		require.NoError(t, err)

		// Count how many applications have reached success state.
		numMatches := 0
		for _, app := range apps.Items {
			c := meta.FindStatusCondition(app.Status.Conditions, string(v1alpha1.BpfAppStateCondSuccess))
			if c != nil && c.Status == metav1.ConditionTrue {
				numMatches++
			}
		}
		// Return true if the number of successful applications matches expected count.
		return numMatches == numExpected
	}
}

// verifyClusterBpfApplicationPriority returns a function that verifies BPF program links are ordered
// correctly according to their priority values on each node.
func verifyClusterBpfApplicationPriority(t *testing.T, labelSelector string) func() bool {
	return func() bool {
		// Fetch all ClusterBpfApplications matching the label selector.
		apps, err := bpfmanClient.BpfmanV1alpha1().ClusterBpfApplications().List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		require.NoError(t, err)

		// Fetch all ClusterBpfApplicationStates to get per-node link information.
		appStates, err := bpfmanClient.BpfmanV1alpha1().ClusterBpfApplicationStates().List(ctx, metav1.ListOptions{})
		require.NoError(t, err)

		// Build a map of node names to their associated links from ClusterBpfApplicationStates.
		nodeLinks := map[string][]link{}
		for _, app := range apps.Items {
			for _, appState := range appStates.Items {
				for _, ownerRef := range appState.OwnerReferences {
					// Skip if this appState is not controlled by the current app.
					if ownerRef.Controller == nil || !*ownerRef.Controller {
						continue
					}
					if ownerRef.UID != app.UID {
						continue
					}
					// Initialize the slice for this node if needed.
					if nodeLinks[appState.Status.Node] == nil {
						nodeLinks[appState.Status.Node] = []link{}
					}
					// Extract and append links from this appState.
					nodeLinks[appState.Status.Node] = append(
						nodeLinks[appState.Status.Node],
						getClusterBpfApplicationStateLinks(t, appState)...,
					)
				}
			}
		}
		// Verify link ordering on each node by directly querying bpfman daemon inside the pod.
		for node, appStateLinks := range nodeLinks {
			bpfmanLinks := []link{}
			// Find the bpfman daemon pod running on this node.
			pods, err := env.Cluster().Client().CoreV1().Pods(bpfmanNamespace).List(ctx, metav1.ListOptions{
				LabelSelector: bpfmanDaemonSelector,
				FieldSelector: fmt.Sprintf("spec.nodeName=%s", node),
			})
			require.NoError(t, err)
			require.Len(t, pods.Items, 1)
			// Query each link from bpfman and verify that bpfman get link matches the output from
			// ClusterBpfApplicationState.
			for _, appStateLink := range appStateLinks {
				cmd := []string{"./bpfman", "get", "link", fmt.Sprintf("%d", appStateLink.linkId)}
				var bpfmanOut, bpfmanErr bytes.Buffer
				err := podExec(ctx, t, pods.Items[0], bpfmanContainer, &bpfmanOut, &bpfmanErr, cmd)
				require.NoError(t, err)
				t.Logf("bpfman get link output:\n%s", bpfmanOut.String())
				// Parse the bpfman output and verify it matches.
				bpfmanLink := parseLink(bpfmanOut.String())
				require.True(t, linkOutputMatchesLink(t, bpfmanLink, appStateLink))
				bpfmanLinks = append(bpfmanLinks, bpfmanLink)
			}
			// Verify that links are ordered correctly by priority (match priority to expected position).
			require.True(t, verifyLinkOrder(bpfmanLinks), "position in slice should match priority", bpfmanLinks)
		}
		return true
	}
}

// link represents a BPF program link with its metadata including link ID, network interface,
// namespace path, priority, and position in the link chain.
type link struct {
	linkId        uint32
	interfaceName string
	netnsPath     string
	priority      int32
	position      int32
}

// parseLink parses the output from "bpfman get link" command and converts it to a link struct.
func parseLink(out string) link {
	l := link{}
	lines := bytes.Split([]byte(out), []byte("\n"))

	for _, line := range lines {
		parts := bytes.SplitN(line, []byte(":"), 2)
		if len(parts) != 2 {
			continue
		}
		key := bytes.TrimSpace(parts[0])
		value := bytes.TrimSpace(parts[1])

		switch string(key) {
		case "Link ID":
			fmt.Sscanf(string(value), "%d", &l.linkId)
		case "Interface":
			l.interfaceName = string(value)
		case "Network Namespace":
			if string(value) != "None" {
				l.netnsPath = string(value)
			}
		case "Priority":
			fmt.Sscanf(string(value), "%d", &l.priority)
		case "Position":
			fmt.Sscanf(string(value), "%d", &l.position)
		}
	}

	return l
}

// getClusterBpfApplicationStateLinks extracts link information from a ClusterBpfApplicationState
// for XDP, TC, and TCX program types.
func getClusterBpfApplicationStateLinks(t *testing.T, appState v1alpha1.ClusterBpfApplicationState) []link {
	links := []link{}
	// Iterate through all programs in the application state.
	for _, program := range appState.Status.Programs {
		switch program.Type {
		case v1alpha1.ProgTypeXDP:
			// Extract XDP program links.
			for _, l := range program.XDP.Links {
				require.NotNil(t, l.LinkId)
				require.NotNil(t, l.Priority)
				links = append(links, link{
					linkId:        *l.LinkId,
					interfaceName: l.InterfaceName,
					netnsPath:     l.NetnsPath,
					priority:      *l.Priority,
				})
			}
		case v1alpha1.ProgTypeTC:
			// Extract TC program links.
			for _, l := range program.TC.Links {
				require.NotNil(t, l.LinkId)
				require.NotNil(t, l.Priority)
				links = append(links, link{
					linkId:        *l.LinkId,
					interfaceName: l.InterfaceName,
					netnsPath:     l.NetnsPath,
					priority:      *l.Priority,
				})
			}
		case v1alpha1.ProgTypeTCX:
			// Extract TCX program links.
			for _, l := range program.TCX.Links {
				require.NotNil(t, l.LinkId)
				require.NotNil(t, l.Priority)
				links = append(links, link{
					linkId:        *l.LinkId,
					interfaceName: l.InterfaceName,
					netnsPath:     l.NetnsPath,
					priority:      *l.Priority,
				})
			}
		}
	}
	return links
}

// linkOutputMatchesLink compares a link parsed from bpfman output with an expected link state.
func linkOutputMatchesLink(t *testing.T, linkFromOutput, l link) bool {
	t.Logf("Comparing output and desired link state; got:\n%+v\nwanted:\n%+v", linkFromOutput, l)
	return l.linkId == linkFromOutput.linkId &&
		l.interfaceName == linkFromOutput.interfaceName &&
		l.netnsPath == linkFromOutput.netnsPath &&
		l.priority == linkFromOutput.priority
}

// verifyLinkOrder verifies that links' positions match their priorities.
// Side-effect: this orders `links` in place by position.
func verifyLinkOrder(links []link) bool {
	// Order elements by position.
	slices.SortFunc(links, func(a, b link) int {
		if a.position < b.position {
			return -1
		}
		if a.position > b.position {
			return 1
		}
		return 0
	})

	// Now, make sure that the priority of each element is >= the preceding element.
	oldI := 0
	for i := 1; i < len(links); i++ {
		if links[i].priority < links[oldI].priority {
			return false
		}
		oldI = i
	}
	return true
}
