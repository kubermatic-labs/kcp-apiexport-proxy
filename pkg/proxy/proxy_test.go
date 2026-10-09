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

package proxy

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"k8s.io/client-go/rest"

	"github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy/lookup"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNewTransport(t *testing.T) {
	transport, err := newTransport(&rest.Config{Host: "https://kcp.example.com", BearerToken: "token"})
	if err != nil {
		t.Fatalf("newTransport: %v", err)
	}
	if transport == nil {
		t.Fatalf("expected a transport")
	}
}

func TestNewTransportInvalidConfig(t *testing.T) {
	_, err := newTransport(&rest.Config{
		Host: "https://kcp.example.com",
		TLSClientConfig: rest.TLSClientConfig{
			CAData: []byte("not a certificate"),
		},
	})
	if err == nil {
		t.Fatalf("expected an error for invalid CA data")
	}
}

func TestShardReverseProxyForwardsToShardURL(t *testing.T) {
	var gotReq *http.Request
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		_, _ = io.WriteString(w, "from shard")
	}))
	defer backend.Close()

	shardURL, err := url.Parse(backend.URL + "/services/apiexport/foo/clusters/1abc/api/v1/configmaps")
	if err != nil {
		t.Fatalf("failed to parse shard URL: %v", err)
	}

	proxy := newShardReverseProxy(http.DefaultTransport)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/clusters/1abc/api/v1/configmaps?limit=10", nil)
	req = req.WithContext(lookup.WithShardURL(req.Context(), shardURL))
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "from shard" {
		t.Fatalf("got body %q, want %q", rec.Body.String(), "from shard")
	}
	if gotReq == nil {
		t.Fatalf("backend did not receive a request")
	}
	if got, want := gotReq.URL.Path, "/services/apiexport/foo/clusters/1abc/api/v1/configmaps"; got != want {
		t.Fatalf("got path %q, want %q", got, want)
	}
	if got, want := gotReq.URL.RawQuery, "limit=10"; got != want {
		t.Fatalf("got query %q, want %q", got, want)
	}
	if gotReq.Header.Get("X-Forwarded-For") == "" {
		t.Fatalf("expected X-Forwarded-For to be set")
	}
}

func TestShardReverseProxyStripsClientCredentials(t *testing.T) {
	var gotHeader http.Header
	transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		gotHeader = req.Header
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
	})

	proxy := newShardReverseProxy(transport)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/clusters/1abc", nil)
	req.Header.Set("Authorization", "Bearer client-token")
	req.Header.Set("Impersonate-User", "admin")
	req.Header.Set("Impersonate-Group", "system:masters")
	req.Header.Set("Accept", "application/json")
	req = req.WithContext(lookup.WithShardURL(req.Context(), &url.URL{Scheme: "https", Host: "shard-a.example.com", Path: "/clusters/1abc"}))

	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", rec.Code, http.StatusOK)
	}
	for _, header := range []string{"Authorization", "Impersonate-User", "Impersonate-Group"} {
		if got := gotHeader.Get(header); got != "" {
			t.Fatalf("expected %s to be removed, got %q", header, got)
		}
	}
	if got := gotHeader.Get("Accept"); got != "application/json" {
		t.Fatalf("expected other headers to be kept, got Accept %q", got)
	}
}

func TestShardReverseProxyWithoutShardURL(t *testing.T) {
	var gotHost string
	transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		gotHost = req.URL.Host
		return nil, errors.New("no such host")
	})

	proxy := newShardReverseProxy(transport)

	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/clusters/1abc", nil))

	if gotHost != "notfound" {
		t.Fatalf("got host %q, want %q", gotHost, "notfound")
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("got status %d, want %d", rec.Code, http.StatusBadGateway)
	}
}
