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

// XdpNsProgramInformer provides access to a shared informer and lister for
// XdpNsPrograms.
type XdpNsProgramInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.XdpNsProgramLister
}

type xdpNsProgramInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewXdpNsProgramInformer constructs a new informer for XdpNsProgram type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewXdpNsProgramInformer(client clientset.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredXdpNsProgramInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredXdpNsProgramInformer constructs a new informer for XdpNsProgram type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredXdpNsProgramInformer(client clientset.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.BpfmanV1alpha1().XdpNsPrograms(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.BpfmanV1alpha1().XdpNsPrograms(namespace).Watch(context.TODO(), options)
			},
		},
		&apisv1alpha1.XdpNsProgram{},
		resyncPeriod,
		indexers,
	)
}

func (f *xdpNsProgramInformer) defaultInformer(client clientset.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredXdpNsProgramInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *xdpNsProgramInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&apisv1alpha1.XdpNsProgram{}, f.defaultInformer)
}

func (f *xdpNsProgramInformer) Lister() v1alpha1.XdpNsProgramLister {
	return v1alpha1.NewXdpNsProgramLister(f.Informer().GetIndexer())
}