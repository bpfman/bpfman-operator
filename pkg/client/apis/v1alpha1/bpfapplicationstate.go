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

// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// BpfApplicationStateLister helps list BpfApplicationStates.
// All objects returned here must be treated as read-only.
type BpfApplicationStateLister interface {
	// List lists all BpfApplicationStates in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.BpfApplicationState, err error)
	// BpfApplicationStates returns an object that can list and get BpfApplicationStates.
	BpfApplicationStates(namespace string) BpfApplicationStateNamespaceLister
	BpfApplicationStateListerExpansion
}

// bpfApplicationStateLister implements the BpfApplicationStateLister interface.
type bpfApplicationStateLister struct {
	indexer cache.Indexer
}

// NewBpfApplicationStateLister returns a new BpfApplicationStateLister.
func NewBpfApplicationStateLister(indexer cache.Indexer) BpfApplicationStateLister {
	return &bpfApplicationStateLister{indexer: indexer}
}

// List lists all BpfApplicationStates in the indexer.
func (s *bpfApplicationStateLister) List(selector labels.Selector) (ret []*v1alpha1.BpfApplicationState, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.BpfApplicationState))
	})
	return ret, err
}

// BpfApplicationStates returns an object that can list and get BpfApplicationStates.
func (s *bpfApplicationStateLister) BpfApplicationStates(namespace string) BpfApplicationStateNamespaceLister {
	return bpfApplicationStateNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// BpfApplicationStateNamespaceLister helps list and get BpfApplicationStates.
// All objects returned here must be treated as read-only.
type BpfApplicationStateNamespaceLister interface {
	// List lists all BpfApplicationStates in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.BpfApplicationState, err error)
	// Get retrieves the BpfApplicationState from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.BpfApplicationState, error)
	BpfApplicationStateNamespaceListerExpansion
}

// bpfApplicationStateNamespaceLister implements the BpfApplicationStateNamespaceLister
// interface.
type bpfApplicationStateNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all BpfApplicationStates in the indexer for a given namespace.
func (s bpfApplicationStateNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.BpfApplicationState, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.BpfApplicationState))
	})
	return ret, err
}

// Get retrieves the BpfApplicationState from the indexer for a given namespace and name.
func (s bpfApplicationStateNamespaceLister) Get(name string) (*v1alpha1.BpfApplicationState, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("bpfapplicationstate"), name)
	}
	return obj.(*v1alpha1.BpfApplicationState), nil
}
