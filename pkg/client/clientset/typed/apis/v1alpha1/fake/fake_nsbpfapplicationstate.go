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

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"

	v1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeNsBpfApplicationStates implements NsBpfApplicationStateInterface
type FakeNsBpfApplicationStates struct {
	Fake *FakeBpfmanV1alpha1
	ns   string
}

var nsbpfapplicationstatesResource = v1alpha1.SchemeGroupVersion.WithResource("nsbpfapplicationstates")

var nsbpfapplicationstatesKind = v1alpha1.SchemeGroupVersion.WithKind("NsBpfApplicationState")

// Get takes name of the nsBpfApplicationState, and returns the corresponding nsBpfApplicationState object, and an error if there is any.
func (c *FakeNsBpfApplicationStates) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.BpfApplicationState, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(nsbpfapplicationstatesResource, c.ns, name), &v1alpha1.BpfApplicationState{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.BpfApplicationState), err
}

// List takes label and field selectors, and returns the list of NsBpfApplicationStates that match those selectors.
func (c *FakeNsBpfApplicationStates) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.BpfApplicationStateList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(nsbpfapplicationstatesResource, nsbpfapplicationstatesKind, c.ns, opts), &v1alpha1.BpfApplicationStateList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.BpfApplicationStateList{ListMeta: obj.(*v1alpha1.BpfApplicationStateList).ListMeta}
	for _, item := range obj.(*v1alpha1.BpfApplicationStateList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested nsBpfApplicationStates.
func (c *FakeNsBpfApplicationStates) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(nsbpfapplicationstatesResource, c.ns, opts))

}

// Create takes the representation of a nsBpfApplicationState and creates it.  Returns the server's representation of the nsBpfApplicationState, and an error, if there is any.
func (c *FakeNsBpfApplicationStates) Create(ctx context.Context, nsBpfApplicationState *v1alpha1.BpfApplicationState, opts v1.CreateOptions) (result *v1alpha1.BpfApplicationState, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(nsbpfapplicationstatesResource, c.ns, nsBpfApplicationState), &v1alpha1.BpfApplicationState{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.BpfApplicationState), err
}

// Update takes the representation of a nsBpfApplicationState and updates it. Returns the server's representation of the nsBpfApplicationState, and an error, if there is any.
func (c *FakeNsBpfApplicationStates) Update(ctx context.Context, nsBpfApplicationState *v1alpha1.BpfApplicationState, opts v1.UpdateOptions) (result *v1alpha1.BpfApplicationState, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(nsbpfapplicationstatesResource, c.ns, nsBpfApplicationState), &v1alpha1.BpfApplicationState{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.BpfApplicationState), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeNsBpfApplicationStates) UpdateStatus(ctx context.Context, nsBpfApplicationState *v1alpha1.BpfApplicationState, opts v1.UpdateOptions) (*v1alpha1.BpfApplicationState, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(nsbpfapplicationstatesResource, "status", c.ns, nsBpfApplicationState), &v1alpha1.BpfApplicationState{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.BpfApplicationState), err
}

// Delete takes name of the nsBpfApplicationState and deletes it. Returns an error if one occurs.
func (c *FakeNsBpfApplicationStates) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteActionWithOptions(nsbpfapplicationstatesResource, c.ns, name, opts), &v1alpha1.BpfApplicationState{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeNsBpfApplicationStates) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(nsbpfapplicationstatesResource, c.ns, listOpts)

	_, err := c.Fake.Invokes(action, &v1alpha1.BpfApplicationStateList{})
	return err
}

// Patch applies the patch and returns the patched nsBpfApplicationState.
func (c *FakeNsBpfApplicationStates) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.BpfApplicationState, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(nsbpfapplicationstatesResource, c.ns, name, pt, data, subresources...), &v1alpha1.BpfApplicationState{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.BpfApplicationState), err
}
