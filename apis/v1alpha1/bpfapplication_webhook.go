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

package v1alpha1

import (
	"context"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var bpfapplicationlog = logf.Log.WithName("bpfapplication-resource")

type BpfApplicationWebhook struct {
	BpfApplication
}

// +kubebuilder:webhook:path=/validate-bpfman-io-v1alpha1-bpfapplication,mutating=false,failurePolicy=fail,sideEffects=None,groups=bpfman.io,resources=bpfapplications,verbs=create;update,versions=v1alpha1,name=vbpfapplication.kb.io,admissionReviewVersions=v1
var _ webhook.CustomValidator = &BpfApplicationWebhook{BpfApplication{}}

func (r *BpfApplicationWebhook) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&BpfApplication{}).
		WithValidator(&BpfApplicationWebhook{}).
		Complete()
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *BpfApplicationWebhook) ValidateCreate(ctx context.Context, obj runtime.Object) error {
	bpfapplicationlog.Info("validate create", "name", r.Name)

	// TODO(user): fill in your validation logic upon object creation.
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *BpfApplicationWebhook) ValidateUpdate(tx context.Context, oldObj runtime.Object, newObj runtime.Object) error {
	bpfapplicationlog.Info("validate update", "name", r.Name)

	// TODO(user): fill in your validation logic upon object update.
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *BpfApplicationWebhook) ValidateDelete(ctx context.Context, newObj runtime.Object) error {
	bpfapplicationlog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil
}
