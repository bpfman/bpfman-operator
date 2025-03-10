/*
Copyright 2025 The bpfman Authors.

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

// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"
	time "time"

	apisv1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	v1alpha1 "github.com/bpfman/bpfman-operator/pkg/client/apis/v1alpha1"
	clientset "github.com/bpfman/bpfman-operator/pkg/client/clientset"
	internalinterfaces "github.com/bpfman/bpfman-operator/pkg/client/externalversions/internalinterfaces"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// BpfApplicationStateInformer provides access to a shared informer and lister for
// BpfApplicationStates.
type BpfApplicationStateInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.BpfApplicationStateLister
}

type bpfApplicationStateInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewBpfApplicationStateInformer constructs a new informer for BpfApplicationState type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewBpfApplicationStateInformer(client clientset.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredBpfApplicationStateInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredBpfApplicationStateInformer constructs a new informer for BpfApplicationState type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredBpfApplicationStateInformer(client clientset.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.BpfmanV1alpha1().BpfApplicationStates(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.BpfmanV1alpha1().BpfApplicationStates(namespace).Watch(context.TODO(), options)
			},
		},
		&apisv1alpha1.BpfApplicationState{},
		resyncPeriod,
		indexers,
	)
}

func (f *bpfApplicationStateInformer) defaultInformer(client clientset.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredBpfApplicationStateInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *bpfApplicationStateInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&apisv1alpha1.BpfApplicationState{}, f.defaultInformer)
}

func (f *bpfApplicationStateInformer) Lister() v1alpha1.BpfApplicationStateLister {
	return v1alpha1.NewBpfApplicationStateLister(f.Informer().GetIndexer())
}
