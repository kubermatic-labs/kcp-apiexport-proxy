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
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

	"github.com/kcp-dev/logicalcluster/v3"
	apisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"

	"github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy/internal/fakekcp"
	"github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy/metrics"
)

func newTestController() *Controller {
	return &Controller{
		clusterURLs: map[logicalcluster.Name]string{},
	}
}

// indexSize returns the endpoints and logical clusters gauges for slice, as
// exposed by the metrics handler.
func indexSize(t *testing.T, slice string) (endpoints, clusters float64) {
	t.Helper()

	rec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))
	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("failed to read metrics: %v", err)
	}

	value := func(name string) float64 {
		prefix := name + `{slice="` + slice + `"} `
		for line := range strings.SplitSeq(string(body), "\n") {
			if v, found := strings.CutPrefix(line, prefix); found {
				f, err := strconv.ParseFloat(v, 64)
				if err != nil {
					t.Fatalf("failed to parse %q: %v", line, err)
				}
				return f
			}
		}
		return -1
	}

	return value("kcp_apiexport_proxy_endpoints"), value("kcp_apiexport_proxy_logical_clusters")
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
	if endpoints, clusters := indexSize(t, "the-slice"); endpoints != 0 || clusters != 0 {
		t.Fatalf("got %v endpoints and %v logical clusters in the metrics after removing all endpoints, want 0 and 0", endpoints, clusters)
	}
}

func TestUpsertAndDeleteBindingIgnoreNonBindings(t *testing.T) {
	c := newTestController()

	c.upsertBinding(&apisv1alpha1.APIExport{}, "https://shard-a.example.com/services/apiexport/foo")
	if len(c.clusterURLs) != 0 {
		t.Fatalf("expected no entries to be recorded for a non-APIBinding object")
	}

	c.clusterURLs["1abc"] = "https://shard-a.example.com/services/apiexport/foo"
	c.deleteBinding(&apisv1alpha1.APIExport{}, "https://shard-a.example.com/services/apiexport/foo")
	c.deleteBinding(&apisv1alpha1.APIBinding{}, "https://shard-a.example.com/services/apiexport/foo")
	if _, found := c.LookupURL("1abc"); !found {
		t.Fatalf("entry evicted by deleting a non-APIBinding or unannotated binding")
	}
}

func TestLookupURLUnknownCluster(t *testing.T) {
	c := newTestController()

	if url, found := c.LookupURL("1abc"); found || url != "" {
		t.Fatalf("got (%q, %v) for an unknown cluster, want (\"\", false)", url, found)
	}
}

func TestSyncEndpointsKeepsExistingInformers(t *testing.T) {
	c, err := NewController(&rest.Config{Host: "https://127.0.0.1:0"}, "the-slice")
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}

	const shardAURL = "https://shard-a.example.com/services/apiexport/foo"
	slice := &apisv1alpha1.APIExportEndpointSlice{
		Status: apisv1alpha1.APIExportEndpointSliceStatus{
			APIExportEndpoints: []apisv1alpha1.APIExportEndpoint{{URL: shardAURL}},
		},
	}

	c.syncEndpoints(slice)
	informer := c.urlBindingInformers[shardAURL]
	c.syncEndpoints(slice)

	if len(c.urlBindingInformers) != 1 {
		t.Fatalf("got %d informers, want 1", len(c.urlBindingInformers))
	}
	if c.urlBindingInformers[shardAURL] != informer {
		t.Fatalf("expected the existing informer to be kept on resync")
	}

	c.syncEndpoints(&apisv1alpha1.APIExportEndpointSlice{})
}

func TestControllerAgainstFakeKCP(t *testing.T) {
	fake := fakekcp.New()
	defer fake.Close()

	fake.AddEndpointSlice("the-slice", "/shard-a", "/shard-b")
	fake.AddBinding("/shard-a", "1abc", "binding-a")
	fake.AddBinding("/shard-b", "1def", "binding-b")

	c, err := NewController(&rest.Config{Host: fake.URL}, "the-slice")
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	if c.HasSynced() {
		t.Fatalf("expected HasSynced to be false before Start")
	}

	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		c.Start(ctx)
	}()

	syncCtx, syncCancel := context.WithTimeout(ctx, 10*time.Second)
	defer syncCancel()
	if !c.WaitForCacheSync(syncCtx.Done()) {
		t.Fatalf("APIExportEndpointSlice informer did not sync")
	}

	want := map[logicalcluster.Name]string{
		"1abc": fake.ShardURL("/shard-a"),
		"1def": fake.ShardURL("/shard-b"),
	}
	err = wait.PollUntilContextCancel(syncCtx, 10*time.Millisecond, true, func(context.Context) (bool, error) {
		for cluster, url := range want {
			if got, _ := c.LookupURL(cluster); got != url {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("index did not converge: %v", err)
	}

	if endpoints, clusters := indexSize(t, "the-slice"); endpoints != 2 || clusters != 2 {
		t.Fatalf("got %v endpoints and %v logical clusters in the metrics, want 2 and 2", endpoints, clusters)
	}

	cancel()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatalf("Start did not return after the context was cancelled")
	}
}
