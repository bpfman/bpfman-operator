/*
Copyright 2023 The bpfman Authors.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1types "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BpfNsApplicationProgramState defines the desired state of BpfNsApplication
// +union
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'XDP' ?  has(self.xdp) : !has(self.xdp)",message="xdp configuration is required when type is XDP, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'TC' ?  has(self.tc) : !has(self.tc)",message="tc configuration is required when type is TC, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'TCX' ?  has(self.tcx) : !has(self.tcx)",message="tcx configuration is required when type is TCX, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'Uprobe' ?  has(self.uprobe) : !has(self.uprobe)",message="uprobe configuration is required when type is Uprobe, and forbidden otherwise"
type BpfNsApplicationProgramState struct {
	BpfProgramStateCommon `json:",inline"`
	// Type specifies the bpf program type
	// +unionDiscriminator
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum:="XDP";"TC";"TCX";"Uprobe"
	Type EBPFProgType `json:"type,omitempty"`

	// xdp defines the desired state of the application's XdpPrograms.
	// +unionMember
	// +optional
	XDP *XdpNsProgramInfoState `json:"xdp,omitempty"`

	// tc defines the desired state of the application's TcPrograms.
	// +unionMember
	// +optional
	TC *TcNsProgramInfoState `json:"tc,omitempty"`

	// tcx defines the desired state of the application's TcxPrograms.
	// +unionMember
	// +optional
	TCX *TcxNsProgramInfoState `json:"tcx,omitempty"`

	// uprobe defines the desired state of the application's UprobePrograms.
	// +unionMember
	// +optional
	Uprobe *UprobeNsProgramInfoState `json:"uprobe,omitempty"`
}

// BpfNsApplicationSpec defines the desired state of BpfNsApplication
type BpfNsApplicationStateSpec struct {
	// Node is the name of the node for this BpfNsApplicationStateSpec.
	Node string `json:"node"`
	// The number of times the BpfNsApplicationState has been updated.  Set to 1
	// when the object is created, then it is incremented prior to each update.
	// This allows us to verify that the API server has the updated object prior
	// to starting a new Reconcile operation.
	UpdateCount int64 `json:"updatecount"`
	// AppLoadStatus reflects the status of loading the bpf application on the
	// given node.
	AppLoadStatus AppLoadStatus `json:"apploadstatus"`
	// Programs is a list of bpf programs contained in the parent application.
	// It is a map from the bpf program name to BpfNsApplicationProgramState
	// elements.
	Programs []BpfNsApplicationProgramState `json:"programs,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced

// BpfNsApplicationState contains the per-node state of a BpfNsApplication.
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=".spec.node"
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.conditions[0].reason`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type BpfNsApplicationState struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BpfNsApplicationStateSpec `json:"spec,omitempty"`
	Status BpfAppStatus              `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// BpfNsApplicationStateList contains a list of BpfNsApplicationState objects
type BpfNsApplicationStateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BpfNsApplicationState `json:"items"`
}

func (an BpfNsApplicationState) GetName() string {
	return an.Name
}

func (an BpfNsApplicationState) GetUID() metav1types.UID {
	return an.UID
}

func (an BpfNsApplicationState) GetAnnotations() map[string]string {
	return an.Annotations
}

func (an BpfNsApplicationState) GetLabels() map[string]string {
	return an.Labels
}

func (an BpfNsApplicationState) GetStatus() *BpfAppStatus {
	return &an.Status
}

func (an BpfNsApplicationState) GetClientObject() client.Object {
	return &an
}

func (anl BpfNsApplicationStateList) GetItems() []BpfNsApplicationState {
	return anl.Items
}
