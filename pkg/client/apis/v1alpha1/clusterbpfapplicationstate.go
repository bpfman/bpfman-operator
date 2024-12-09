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

// ClusterBpfApplicationStateLister helps list ClusterBpfApplicationStates.
// All objects returned here must be treated as read-only.
type ClusterBpfApplicationStateLister interface {
	// List lists all ClusterBpfApplicationStates in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.ClusterBpfApplicationState, err error)
	// Get retrieves the ClusterBpfApplicationState from the index for a given name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.ClusterBpfApplicationState, error)
	ClusterBpfApplicationStateListerExpansion
}

// clusterBpfApplicationStateLister implements the ClusterBpfApplicationStateLister interface.
type clusterBpfApplicationStateLister struct {
	indexer cache.Indexer
}

// NewClusterBpfApplicationStateLister returns a new ClusterBpfApplicationStateLister.
func NewClusterBpfApplicationStateLister(indexer cache.Indexer) ClusterBpfApplicationStateLister {
	return &clusterBpfApplicationStateLister{indexer: indexer}
}

// List lists all ClusterBpfApplicationStates in the indexer.
func (s *clusterBpfApplicationStateLister) List(selector labels.Selector) (ret []*v1alpha1.ClusterBpfApplicationState, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.ClusterBpfApplicationState))
	})
	return ret, err
}

// Get retrieves the ClusterBpfApplicationState from the index for a given name.
func (s *clusterBpfApplicationStateLister) Get(name string) (*v1alpha1.ClusterBpfApplicationState, error) {
	obj, exists, err := s.indexer.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("clusterbpfapplicationstate"), name)
	}
	return obj.(*v1alpha1.ClusterBpfApplicationState), nil
}
