/*
Copyright 2024.

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

type NamespaceProgramReconciler struct {
	ReconcilerCommon[bpfmaniov1alpha1.BpfNsProgram, bpfmaniov1alpha1.BpfNsProgramList]
}

//lint:ignore U1000 Linter claims function unused, but generics confusing linter
func (r *NamespaceProgramReconciler) getBpfList(
	ctx context.Context,
	progName string,
	progNamespace string,
) (*bpfmaniov1alpha1.BpfNsProgramList, error) {

	bpfProgramList := &bpfmaniov1alpha1.BpfNsProgramList{}

	// Only list bpfPrograms for this Program
	opts := []client.ListOption{
		client.MatchingLabels{internal.BpfProgramOwner: progName},
		client.InNamespace(progNamespace),
	}

	err := r.List(ctx, bpfProgramList, opts...)
	if err != nil {
		return nil, err
	}

	return bpfProgramList, nil
}

//lint:ignore U1000 Linter claims function unused, but generics confusing linter
func (r *NamespaceProgramReconciler) containsFinalizer(
	bpfProgram *bpfmaniov1alpha1.BpfNsProgram,
	finalizer string,
) bool {
	return controllerutil.ContainsFinalizer(bpfProgram, finalizer)
}

func statusChangedPredicateNamespace() predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObject := e.ObjectOld.(*bpfmaniov1alpha1.BpfNsProgram)
			newObject := e.ObjectNew.(*bpfmaniov1alpha1.BpfNsProgram)
			return !reflect.DeepEqual(oldObject.GetStatus(), newObject.Status)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
	}
}
