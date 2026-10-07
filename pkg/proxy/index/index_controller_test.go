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

package index

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	"github.com/kcp-dev/logicalcluster/v3"
	apisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
)

func newTestController() *Controller {
	return &Controller{
		clusterURLs: map[logicalcluster.Name]string{},
	}
}

func binding(cluster, name string) *apisv1alpha1.APIBinding {
	return &apisv1alpha1.APIBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: map[string]string{logicalcluster.AnnotationKey: cluster},
		},
	}
}

func TestUpsertAndDeleteBinding(t *testing.T) {
	c := newTestController()

	c.upsertBinding(binding("1abc", "my-binding"), "https://shard-a.example.com/services/apiexport/foo")

	url, found := c.LookupURL("1abc")
	if !found {
		t.Fatalf("expected cluster 1abc to be found after upsert")
	}
	if url != "https://shard-a.example.com/services/apiexport/foo" {
		t.Fatalf("got url %q, want shard-a URL", url)
	}

	// A second binding on the same cluster/shard is a no-op change.
	c.upsertBinding(binding("1abc", "another-binding"), "https://shard-a.example.com/services/apiexport/foo")
	if url, _ := c.LookupURL("1abc"); url != "https://shard-a.example.com/services/apiexport/foo" {
		t.Fatalf("url changed unexpectedly: %q", url)
	}

	// Deleting from a different URL than the one on record must not evict
	// the (still valid) entry.
	c.deleteBinding(binding("1abc", "my-binding"), "https://shard-b.example.com/services/apiexport/foo")
	if _, found := c.LookupURL("1abc"); !found {
		t.Fatalf("entry evicted by delete from the wrong shard URL")
	}

	c.deleteBinding(binding("1abc", "my-binding"), "https://shard-a.example.com/services/apiexport/foo")
	if _, found := c.LookupURL("1abc"); found {
		t.Fatalf("expected cluster 1abc to be gone after delete")
	}
}

func TestUpsertBindingWithoutClusterAnnotation(t *testing.T) {
	c := newTestController()

	c.upsertBinding(&apisv1alpha1.APIBinding{}, "https://shard-a.example.com/services/apiexport/foo")

	if len(c.clusterURLs) != 0 {
		t.Fatalf("expected no entries to be recorded for a binding without a cluster annotation")
	}
}

func TestDeleteURLLocked(t *testing.T) {
	c := newTestController()

	c.upsertBinding(binding("1abc", "b1"), "https://shard-a.example.com/services/apiexport/foo")
	c.upsertBinding(binding("1def", "b2"), "https://shard-b.example.com/services/apiexport/foo")
	c.upsertBinding(binding("1ghi", "b3"), "https://shard-a.example.com/services/apiexport/foo")

	c.lock.Lock()
	c.deleteURLLocked("https://shard-a.example.com/services/apiexport/foo")
	c.lock.Unlock()

	if _, found := c.LookupURL("1abc"); found {
		t.Fatalf("expected 1abc to be evicted along with shard-a's URL")
	}
	if _, found := c.LookupURL("1ghi"); found {
		t.Fatalf("expected 1ghi to be evicted along with shard-a's URL")
	}
	if _, found := c.LookupURL("1def"); !found {
		t.Fatalf("expected 1def (shard-b) to survive shard-a's removal")
	}
}

func TestSyncEndpointsStartsAndStopsPerURLInformers(t *testing.T) {
	c, err := NewController(&rest.Config{Host: "https://127.0.0.1:0"}, "the-slice")
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}

	const shardAURL = "https://shard-a.example.com/services/apiexport/foo"
	const shardBURL = "https://shard-b.example.com/services/apiexport/foo"

	c.syncEndpoints(&apisv1alpha1.APIExportEndpointSlice{
		Status: apisv1alpha1.APIExportEndpointSliceStatus{
			APIExportEndpoints: []apisv1alpha1.APIExportEndpoint{{URL: shardAURL}, {URL: shardBURL}},
		},
	})
	if len(c.urlBindingInformers) != 2 {
		t.Fatalf("got %d informers after adding two endpoints, want 2", len(c.urlBindingInformers))
	}

	// Simulate a binding having previously been observed via shard A, so we
	// can verify that removing shard A's URL evicts it.
	c.lock.Lock()
	c.clusterURLs["1abc"] = shardAURL
	c.lock.Unlock()

	c.syncEndpoints(&apisv1alpha1.APIExportEndpointSlice{
		Status: apisv1alpha1.APIExportEndpointSliceStatus{
			APIExportEndpoints: []apisv1alpha1.APIExportEndpoint{{URL: shardBURL}},
		},
	})
	if len(c.urlBindingInformers) != 1 {
		t.Fatalf("got %d informers after removing one endpoint, want 1", len(c.urlBindingInformers))
	}
	if _, found := c.urlBindingInformers[shardBURL]; !found {
		t.Fatalf("expected shard B's informer to survive")
	}
	if _, found := c.LookupURL("1abc"); found {
		t.Fatalf("expected 1abc to be evicted when shard A's URL was removed")
	}

	// Clean up the remaining informer's goroutine.
	c.syncEndpoints(&apisv1alpha1.APIExportEndpointSlice{})
	if len(c.urlBindingInformers) != 0 {
		t.Fatalf("got %d informers after removing all endpoints, want 0", len(c.urlBindingInformers))
	}
}
