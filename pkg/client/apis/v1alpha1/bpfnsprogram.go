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

// BpfNsProgramLister helps list BpfNsPrograms.
// All objects returned here must be treated as read-only.
type BpfNsProgramLister interface {
	// List lists all BpfNsPrograms in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.BpfNsProgram, err error)
	// BpfNsPrograms returns an object that can list and get BpfNsPrograms.
	BpfNsPrograms(namespace string) BpfNsProgramNamespaceLister
	BpfNsProgramListerExpansion
}

// bpfNsProgramLister implements the BpfNsProgramLister interface.
type bpfNsProgramLister struct {
	indexer cache.Indexer
}

// NewBpfNsProgramLister returns a new BpfNsProgramLister.
func NewBpfNsProgramLister(indexer cache.Indexer) BpfNsProgramLister {
	return &bpfNsProgramLister{indexer: indexer}
}

// List lists all BpfNsPrograms in the indexer.
func (s *bpfNsProgramLister) List(selector labels.Selector) (ret []*v1alpha1.BpfNsProgram, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.BpfNsProgram))
	})
	return ret, err
}

// BpfNsPrograms returns an object that can list and get BpfNsPrograms.
func (s *bpfNsProgramLister) BpfNsPrograms(namespace string) BpfNsProgramNamespaceLister {
	return bpfNsProgramNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// BpfNsProgramNamespaceLister helps list and get BpfNsPrograms.
// All objects returned here must be treated as read-only.
type BpfNsProgramNamespaceLister interface {
	// List lists all BpfNsPrograms in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.BpfNsProgram, err error)
	// Get retrieves the BpfNsProgram from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.BpfNsProgram, error)
	BpfNsProgramNamespaceListerExpansion
}

// bpfNsProgramNamespaceLister implements the BpfNsProgramNamespaceLister
// interface.
type bpfNsProgramNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all BpfNsPrograms in the indexer for a given namespace.
func (s bpfNsProgramNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.BpfNsProgram, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.BpfNsProgram))
	})
	return ret, err
}

// Get retrieves the BpfNsProgram from the indexer for a given namespace and name.
func (s bpfNsProgramNamespaceLister) Get(name string) (*v1alpha1.BpfNsProgram, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("bpfnsprogram"), name)
	}
	return obj.(*v1alpha1.BpfNsProgram), nil
}