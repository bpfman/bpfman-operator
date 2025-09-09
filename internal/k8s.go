/*
Copyright 2022.

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

package internal

import (
	"github.com/bpfman/bpfman-operator/apis/v1alpha1"
	"github.com/go-logr/logr"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// Only reconcile if a program has been created for a controller's node.
func BpfNodePredicate(nodeName string) predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.GetLabels()[K8sHostLabel] == nodeName
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetLabels()[K8sHostLabel] == nodeName
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetLabels()[K8sHostLabel] == nodeName
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetLabels()[K8sHostLabel] == nodeName
		},
	}
}

// Only reconcile if a bpfprogram has been created for a controller's node.
func DiscoveredBpfProgramPredicate() predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			_, ok := e.Object.GetLabels()[DiscoveredLabel]
			return ok
		},
		CreateFunc: func(e event.CreateEvent) bool {
			_, ok := e.Object.GetLabels()[DiscoveredLabel]
			return ok
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			_, ok := e.ObjectNew.GetLabels()[DiscoveredLabel]
			return ok
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			_, ok := e.Object.GetLabels()[DiscoveredLabel]
			return ok
		},
	}
}

// Returns true if the current platform is Openshift.
func IsOpenShift(client discovery.DiscoveryInterface, setupLog logr.Logger) (bool, error) {
	k8sVersion, err := client.ServerVersion()
	if err != nil {
		setupLog.Info("issue occurred while fetching ServerVersion")
		return false, err
	}

	setupLog.Info("detected platform version", "PlatformVersion", k8sVersion)
	apiList, err := client.ServerGroups()
	if err != nil {
		setupLog.Info("issue occurred while fetching ServerGroups")
		return false, err
	}

	for _, v := range apiList.Groups {
		if v.Name == "route.openshift.io" {
			setupLog.Info("route.openshift.io found in apis, platform is OpenShift")
			return true, nil
		}
	}
	return false, nil
}

// CCSEquals compares two map[string]ConfigComponentStatus instances for equality.
func CCSEquals(ccs, c map[string]v1alpha1.ConfigComponentStatus) bool {
	if len(ccs) != len(c) {
		return false
	}
	for k, v := range ccs {
		if c[k] != v {
			return false
		}
	}
	return true
}

// CCSAnyComponentProgressing returns true if any component is in the Progressing state.
func CCSAnyComponentProgressing(ccs map[string]v1alpha1.ConfigComponentStatus, isOpenShift bool) bool {
	requiredComponents := []string{"ConfigMap", "DaemonSet", "MetricsProxyDaemonSet", "CsiDriver"}
	if isOpenShift {
		requiredComponents = append(requiredComponents, "Scc")
	}

	for _, component := range requiredComponents {
		if status, ok := ccs[component]; ok && status == v1alpha1.ConfigStatusProgressing {
			return true
		}
	}
	return false
}

// CCSAllComponentsReady returns true if all components are in the Ready state.
func CCSAllComponentsReady(ccs map[string]v1alpha1.ConfigComponentStatus, isOpenShift bool) bool {
	requiredComponents := []string{"ConfigMap", "DaemonSet", "MetricsProxyDaemonSet", "CsiDriver"}
	if isOpenShift {
		requiredComponents = append(requiredComponents, "Scc")
	}

	for _, component := range requiredComponents {
		status, ok := ccs[component]
		if !ok || status != v1alpha1.ConfigStatusReady {
			return false
		}
	}
	return true
}
