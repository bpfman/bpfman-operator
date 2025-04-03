/*
Copyright 2024 The bpfman Authors.

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

package bpfmanoperator

import (
	"context"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	internal "github.com/bpfman/bpfman-operator/internal"
)

type ClusterApplicationReconciler struct {
	ReconcilerCommon[bpfmaniov1alpha1.ClusterBpfApplicationState, bpfmaniov1alpha1.ClusterBpfApplicationStateList]
}

//lint:ignore U1000 Linter claims function unused, but generics confusing linter
func (r *ClusterApplicationReconciler) getAppStateList(
	ctx context.Context,
	appName string,
	_appNamespace string,
) (*bpfmaniov1alpha1.ClusterBpfApplicationStateList, error) {

	appStateList := &bpfmaniov1alpha1.ClusterBpfApplicationStateList{}

	// Only list BpfApplicationState objects for this BpfApplication
	opts := []client.ListOption{
		client.MatchingLabels{internal.BpfAppStateOwner: appName},
	}

	err := r.List(ctx, appStateList, opts...)
	if err != nil {
		return nil, err
	}

	return appStateList, nil
}

//lint:ignore U1000 Linter claims function unused, but generics confusing linter
func (r *ClusterApplicationReconciler) containsFinalizer(
	bpfAppState *bpfmaniov1alpha1.ClusterBpfApplicationState,
	finalizer string,
) bool {
	return controllerutil.ContainsFinalizer(bpfAppState, finalizer)
}

func statusChangedPredicateCluster() predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObject := e.ObjectOld.(*bpfmaniov1alpha1.ClusterBpfApplicationState)
			newObject := e.ObjectNew.(*bpfmaniov1alpha1.ClusterBpfApplicationState)
			statusChanged := !reflect.DeepEqual(oldObject.Status.Conditions, newObject.Status.Conditions)
			finalizerChanged := controllerutil.ContainsFinalizer(oldObject, internal.ClBpfApplicationControllerFinalizer) !=
				controllerutil.ContainsFinalizer(newObject, internal.ClBpfApplicationControllerFinalizer)
			return statusChanged || finalizerChanged
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
	}
}
