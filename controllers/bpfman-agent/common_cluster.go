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

//lint:file-ignore U1000 Linter claims functions unused, but are required for generic

package bpfmanagent

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	"github.com/bpfman/bpfman-operator/internal"
)

type ClusterProgramReconciler struct {
	ReconcilerCommon[bpfmaniov1alpha1.BpfProgram, bpfmaniov1alpha1.BpfProgramList]
}

// createBpfProgram moves some shared logic for building bpfProgram objects
// into a central location.
func (r *ClusterProgramReconciler) createBpfProgram(
	attachPoint string,
	rec bpfmanReconciler[bpfmaniov1alpha1.BpfProgram, bpfmaniov1alpha1.BpfProgramList],
	annotations map[string]string,
) (*bpfmaniov1alpha1.BpfProgram, error) {

	r.Logger.V(1).Info("createBpfProgram()", "Name", attachPoint,
		"Owner", rec.getOwner().GetName(), "OwnerType", rec.getRecType(), "Name", rec.getName())

	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[internal.BpfProgramAttachPoint] = attachPoint

	bpfProg := &bpfmaniov1alpha1.BpfProgram{
		ObjectMeta: metav1.ObjectMeta{
			Name:       generateUniqueName(rec.getName()),
			Finalizers: []string{rec.getFinalizer()},
			Labels: map[string]string{
				internal.BpfProgramOwner: rec.getOwner().GetName(),
				internal.AppProgramId:    rec.getAppProgramId(),
				internal.K8sHostLabel:    r.NodeName,
			},
			Annotations: annotations,
		},
		Spec: bpfmaniov1alpha1.BpfProgramSpec{
			Type: rec.getRecType(),
		},
		Status: bpfmaniov1alpha1.BpfProgramStatus{Conditions: []metav1.Condition{}},
	}

	// Make the corresponding BpfProgramConfig the owner
	if err := ctrl.SetControllerReference(rec.getOwner(), bpfProg, r.Scheme); err != nil {
		return nil, fmt.Errorf("failed to bpfProgram object owner reference: %v", err)
	}

	return bpfProg, nil
}

func (r *ClusterProgramReconciler) getBpfList(
	ctx context.Context,
	opts []client.ListOption,
) (*bpfmaniov1alpha1.BpfProgramList, error) {

	bpfProgramList := &bpfmaniov1alpha1.BpfProgramList{}

	err := r.List(ctx, bpfProgramList, opts...)
	if err != nil {
		return nil, err
	}

	return bpfProgramList, nil
}

func (r *ClusterProgramReconciler) updateBpfStatus(
	ctx context.Context,
	bpfProgram *bpfmaniov1alpha1.BpfProgram,
	condition metav1.Condition,
) error {
	bpfProgram.Status.Conditions = nil
	meta.SetStatusCondition(&bpfProgram.Status.Conditions, condition)
	return r.Status().Update(ctx, bpfProgram)
}
