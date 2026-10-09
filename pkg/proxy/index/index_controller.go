/*
Copyright The kcp-apiexport-proxy Authors.

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

// Package index builds and maintains a mapping from logical cluster to the
// shard-specific APIExportEndpointSlice virtual workspace URL that serves
// it.
//
// The mapping is derived, not configured: each shard's virtual workspace URL
// is backed only by that shard's own (non-cache-server) informers, so a
// wildcard watch for APIBindings issued through a given URL only ever
// surfaces APIBindings that live in logical clusters actually hosted on that
// shard. Watching APIBindings through every URL published by the
// APIExportEndpointSlice therefore reveals which logical cluster belongs
// behind which URL, without ever needing to watch Shard or LogicalCluster
// objects directly.
package index

import (
	"context"
	"fmt"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kcp-dev/logicalcluster/v3"
	apisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpclientset "github.com/kcp-dev/sdk/client/clientset/versioned"
	kcpclusterclientset "github.com/kcp-dev/sdk/client/clientset/versioned/cluster"
	apisv1alpha1informers "github.com/kcp-dev/sdk/client/informers/externalversions/apis/v1alpha1"

	"github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy/metrics"
)

// resyncPeriod is hardcoded rather than exposed as a flag, mirroring
// upstream/pkg/proxy/index's resyncPeriod constant.
const resyncPeriod = 10 * time.Hour

// Index maps a logical cluster to the shard-specific APIExportEndpointSlice
// virtual workspace URL that serves it.
type Index interface {
	LookupURL(cluster logicalcluster.Name) (string, bool)
}

// Controller watches a single named APIExportEndpointSlice for its list of
// shard virtual workspace URLs, and for each URL watches APIBindings to
// learn which logical clusters live behind it.
type Controller struct {
	identityConfig *rest.Config
	sliceName      string

	homeClient kcpclientset.Interface

	sliceInformer cache.SharedIndexInformer

	lock                sync.RWMutex
	urlBindingInformers map[string]cache.SharedIndexInformer
	urlBindingStopCh    map[string]chan struct{}

	clusterURLs map[logicalcluster.Name]string
}

// NewController creates a Controller. identityConfig is used both to reach
// the home cluster (where the named APIExportEndpointSlice lives, via its
// current context) and, with its Host swapped per shard, to watch
// APIBindings through each shard's virtual workspace URL.
func NewController(identityConfig *rest.Config, sliceName string) (*Controller, error) {
	homeClient, err := kcpclientset.NewForConfig(identityConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for home cluster: %w", err)
	}

	c := &Controller{
		identityConfig: identityConfig,
		sliceName:      sliceName,

		homeClient: homeClient,

		urlBindingInformers: map[string]cache.SharedIndexInformer{},
		urlBindingStopCh:    map[string]chan struct{}{},

		clusterURLs: map[logicalcluster.Name]string{},
	}

	c.sliceInformer = apisv1alpha1informers.NewFilteredAPIExportEndpointSliceInformer(
		homeClient,
		resyncPeriod,
		cache.Indexers{},
		func(opts *metav1.ListOptions) {
			opts.FieldSelector = fields.OneTermEqualSelector("metadata.name", sliceName).String()
		},
	)

	_, _ = c.sliceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.syncEndpoints(obj.(*apisv1alpha1.APIExportEndpointSlice))
		},
		UpdateFunc: func(_, obj interface{}) {
			c.syncEndpoints(obj.(*apisv1alpha1.APIExportEndpointSlice))
		},
		DeleteFunc: func(interface{}) {
			c.syncEndpoints(&apisv1alpha1.APIExportEndpointSlice{})
		},
	})

	return c, nil
}

// Start runs the controller until ctx is cancelled.
func (c *Controller) Start(ctx context.Context) {
	defer utilruntime.HandleCrash()

	logger := klog.FromContext(ctx).WithValues("controller", "apiexport-endpointslice-index")
	logger.Info("Starting controller")
	defer logger.Info("Shutting down controller")

	go c.sliceInformer.Run(ctx.Done())

	<-ctx.Done()

	c.lock.Lock()
	defer c.lock.Unlock()
	for _, stopCh := range c.urlBindingStopCh {
		close(stopCh)
	}
}

// WaitForCacheSync blocks until the APIExportEndpointSlice informer has
// synced, or stopCh is closed.
func (c *Controller) WaitForCacheSync(stopCh <-chan struct{}) bool {
	return cache.WaitForCacheSync(stopCh, c.sliceInformer.HasSynced)
}

// HasSynced reports, without blocking, whether the APIExportEndpointSlice
// informer has synced at least once.
func (c *Controller) HasSynced() bool {
	return c.sliceInformer.HasSynced()
}

// syncEndpoints reconciles the set of per-URL APIBinding informers against
// the APIExportEndpointSlice's current list of endpoint URLs.
func (c *Controller) syncEndpoints(slice *apisv1alpha1.APIExportEndpointSlice) {
	wanted := make(map[string]bool, len(slice.Status.APIExportEndpoints))
	for _, ep := range slice.Status.APIExportEndpoints {
		wanted[ep.URL] = true
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	for url, stopCh := range c.urlBindingStopCh {
		if wanted[url] {
			continue
		}

		close(stopCh)
		delete(c.urlBindingStopCh, url)
		delete(c.urlBindingInformers, url)
		c.deleteURLLocked(url)
	}

	for url := range wanted {
		if _, found := c.urlBindingInformers[url]; found {
			continue
		}

		if err := c.startURLInformerLocked(url); err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to start APIBindings watch for endpoint %q: %w", url, err))
		}
	}

	c.updateMetricsLocked()
}

// startURLInformerLocked starts a wildcard APIBindings informer against the
// given shard virtual workspace URL. c.lock must be held by the caller.
func (c *Controller) startURLInformerLocked(url string) error {
	shardConfig := rest.CopyConfig(c.identityConfig)
	shardConfig.Host = url

	shardClient, err := kcpclusterclientset.NewForConfig(shardConfig)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	informer := apisv1alpha1informers.NewAPIBindingClusterInformer(shardClient, resyncPeriod, cache.Indexers{})

	_, _ = informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.upsertBinding(obj, url)
		},
		UpdateFunc: func(_, obj interface{}) {
			c.upsertBinding(obj, url)
		},
		DeleteFunc: func(obj interface{}) {
			if final, ok := obj.(cache.DeletedFinalStateUnknown); ok {
				obj = final.Obj
			}
			c.deleteBinding(obj, url)
		},
	})

	stopCh := make(chan struct{})
	c.urlBindingInformers[url] = informer
	c.urlBindingStopCh[url] = stopCh
	go informer.Run(stopCh)

	return nil
}

func (c *Controller) upsertBinding(obj interface{}, url string) {
	binding, ok := obj.(*apisv1alpha1.APIBinding)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("obj is supposed to be an APIBinding, but is %T", obj))
		return
	}

	cluster := logicalcluster.From(binding)
	if cluster.Empty() {
		return
	}

	c.lock.Lock()
	defer c.lock.Unlock()
	c.clusterURLs[cluster] = url
	c.updateMetricsLocked()
}

func (c *Controller) deleteBinding(obj interface{}, url string) {
	binding, ok := obj.(*apisv1alpha1.APIBinding)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("obj is supposed to be an APIBinding, but is %T", obj))
		return
	}

	cluster := logicalcluster.From(binding)
	if cluster.Empty() {
		return
	}

	c.lock.Lock()
	defer c.lock.Unlock()
	if c.clusterURLs[cluster] == url {
		delete(c.clusterURLs, cluster)
	}
	c.updateMetricsLocked()
}

// updateMetricsLocked records the current size of the index. c.lock must be
// held by the caller.
func (c *Controller) updateMetricsLocked() {
	metrics.SetIndexSize(c.sliceName, len(c.urlBindingInformers), len(c.clusterURLs))
}

// deleteURLLocked removes every index entry pointing at url. c.lock must be
// held by the caller.
func (c *Controller) deleteURLLocked(url string) {
	for cluster, u := range c.clusterURLs {
		if u == url {
			delete(c.clusterURLs, cluster)
		}
	}
}

// LookupURL returns the shard virtual workspace URL that serves the given
// logical cluster, if known.
func (c *Controller) LookupURL(cluster logicalcluster.Name) (string, bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	url, found := c.clusterURLs[cluster]
	return url, found
}
