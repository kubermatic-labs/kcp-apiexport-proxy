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

// Package fakekcp provides a minimal fake of the kcp endpoints used by the
// proxy, for use in tests only. It serves a static APIExportEndpointSlice
// list and static per-shard wildcard APIBinding lists (both list and
// watch), and hands every other request to an optional fallback handler.
package fakekcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/kcp-dev/logicalcluster/v3"
	apisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
)

const (
	slicesPath          = "/apis/apis.kcp.io/v1alpha1/apiexportendpointslices"
	wildcardBindingPath = "/clusters/*/apis/apis.kcp.io/v1alpha1/apibindings"
	resourceVersion     = "1"
)

// Server is a fake kcp server. Configure it before starting anything that
// talks to it.
type Server struct {
	*httptest.Server

	lock     sync.Mutex
	slices   []apisv1alpha1.APIExportEndpointSlice
	bindings map[string][]apisv1alpha1.APIBinding
	fallback http.Handler

	done      chan struct{}
	closeOnce sync.Once
}

// New starts a new fake kcp server.
func New() *Server {
	s := &Server{
		bindings: map[string][]apisv1alpha1.APIBinding{},
		done:     make(chan struct{}),
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serveHTTP))
	return s
}

// Close unblocks all open watches and shuts down the server.
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		close(s.done)
		s.Server.Close()
	})
}

// AddEndpointSlice adds an APIExportEndpointSlice with the given name whose
// endpoints are the server URL joined with each of the given shard paths.
func (s *Server) AddEndpointSlice(name string, shardPaths ...string) {
	slice := apisv1alpha1.APIExportEndpointSlice{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apisv1alpha1.SchemeGroupVersion.String(),
			Kind:       "APIExportEndpointSlice",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			ResourceVersion: resourceVersion,
		},
	}
	for _, p := range shardPaths {
		slice.Status.APIExportEndpoints = append(slice.Status.APIExportEndpoints, apisv1alpha1.APIExportEndpoint{
			URL: s.ShardURL(p),
		})
	}

	s.lock.Lock()
	defer s.lock.Unlock()
	s.slices = append(s.slices, slice)
}

// AddBinding adds an APIBinding in the given logical cluster, visible through
// the shard virtual workspace at shardPath.
func (s *Server) AddBinding(shardPath string, cluster logicalcluster.Name, name string) {
	binding := apisv1alpha1.APIBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apisv1alpha1.SchemeGroupVersion.String(),
			Kind:       "APIBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			ResourceVersion: resourceVersion,
			Annotations:     map[string]string{logicalcluster.AnnotationKey: cluster.String()},
		},
	}

	s.lock.Lock()
	defer s.lock.Unlock()
	s.bindings[shardPath] = append(s.bindings[shardPath], binding)
}

// SetFallback sets the handler for all requests the fake doesn't serve
// itself.
func (s *Server) SetFallback(h http.Handler) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.fallback = h
}

// ShardURL returns the virtual workspace URL for the given shard path.
func (s *Server) ShardURL(shardPath string) string {
	return s.URL + shardPath
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	s.lock.Lock()
	defer s.lock.Unlock()

	switch {
	case r.URL.Path == slicesPath:
		list := &apisv1alpha1.APIExportEndpointSliceList{
			TypeMeta: metav1.TypeMeta{APIVersion: apisv1alpha1.SchemeGroupVersion.String(), Kind: "APIExportEndpointSliceList"},
			ListMeta: metav1.ListMeta{ResourceVersion: resourceVersion},
			Items:    s.slices,
		}
		var items []runtime.Object
		for i := range s.slices {
			items = append(items, &s.slices[i])
		}
		bookmark := &apisv1alpha1.APIExportEndpointSlice{TypeMeta: metav1.TypeMeta{APIVersion: apisv1alpha1.SchemeGroupVersion.String(), Kind: "APIExportEndpointSlice"}}
		s.serveListOrWatch(w, r, list, items, bookmark)

	case strings.HasSuffix(r.URL.Path, wildcardBindingPath):
		bindings := s.bindings[strings.TrimSuffix(r.URL.Path, wildcardBindingPath)]
		list := &apisv1alpha1.APIBindingList{
			TypeMeta: metav1.TypeMeta{APIVersion: apisv1alpha1.SchemeGroupVersion.String(), Kind: "APIBindingList"},
			ListMeta: metav1.ListMeta{ResourceVersion: resourceVersion},
			Items:    bindings,
		}
		var items []runtime.Object
		for i := range bindings {
			items = append(items, &bindings[i])
		}
		bookmark := &apisv1alpha1.APIBinding{TypeMeta: metav1.TypeMeta{APIVersion: apisv1alpha1.SchemeGroupVersion.String(), Kind: "APIBinding"}}
		s.serveListOrWatch(w, r, list, items, bookmark)

	case s.fallback != nil:
		fallback := s.fallback
		s.lock.Unlock()
		fallback.ServeHTTP(w, r)
		s.lock.Lock()

	default:
		http.NotFound(w, r)
	}
}

type watchEvent struct {
	Type   string          `json:"type"`
	Object json.RawMessage `json:"object"`
}

// serveListOrWatch serves a list, or a watch that optionally streams the
// initial items (for WatchList clients) and then blocks until the client or
// the server goes away. s.lock is held on entry and on return.
func (s *Server) serveListOrWatch(w http.ResponseWriter, r *http.Request, list runtime.Object, items []runtime.Object, bookmark metav1.Object) {
	w.Header().Set("Content-Type", "application/json")

	if r.URL.Query().Get("watch") != "true" {
		_ = json.NewEncoder(w).Encode(list)
		return
	}

	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)

	if r.URL.Query().Get("sendInitialEvents") == "true" {
		for _, item := range items {
			raw, _ := json.Marshal(item)
			_ = enc.Encode(watchEvent{Type: "ADDED", Object: raw})
		}

		bookmark.SetResourceVersion(resourceVersion)
		bookmark.SetAnnotations(map[string]string{metav1.InitialEventsAnnotationKey: "true"})
		raw, _ := json.Marshal(bookmark)
		_ = enc.Encode(watchEvent{Type: "BOOKMARK", Object: raw})
	}

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	s.lock.Unlock()
	defer s.lock.Lock()

	select {
	case <-r.Context().Done():
	case <-s.done:
	}
}
