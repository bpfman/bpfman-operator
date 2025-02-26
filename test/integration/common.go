//go:build integration_tests
// +build integration_tests

package integration

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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
