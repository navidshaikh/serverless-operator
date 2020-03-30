// This file was automatically generated by informer-gen

package v1

import (
	network_v1 "github.com/openshift/api/network/v1"
	versioned "github.com/openshift/client-go/network/clientset/versioned"
	internalinterfaces "github.com/openshift/client-go/network/informers/externalversions/internalinterfaces"
	v1 "github.com/openshift/client-go/network/listers/network/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
	time "time"
)

// HostSubnetInformer provides access to a shared informer and lister for
// HostSubnets.
type HostSubnetInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1.HostSubnetLister
}

type hostSubnetInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
}

// NewHostSubnetInformer constructs a new informer for HostSubnet type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewHostSubnetInformer(client versioned.Interface, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredHostSubnetInformer(client, resyncPeriod, indexers, nil)
}

// NewFilteredHostSubnetInformer constructs a new informer for HostSubnet type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredHostSubnetInformer(client versioned.Interface, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options meta_v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.NetworkV1().HostSubnets().List(options)
			},
			WatchFunc: func(options meta_v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.NetworkV1().HostSubnets().Watch(options)
			},
		},
		&network_v1.HostSubnet{},
		resyncPeriod,
		indexers,
	)
}

func (f *hostSubnetInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredHostSubnetInformer(client, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *hostSubnetInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&network_v1.HostSubnet{}, f.defaultInformer)
}

func (f *hostSubnetInformer) Lister() v1.HostSubnetLister {
	return v1.NewHostSubnetLister(f.Informer().GetIndexer())
}
