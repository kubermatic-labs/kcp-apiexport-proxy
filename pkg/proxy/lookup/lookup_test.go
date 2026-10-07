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

package lookup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/kcp-dev/logicalcluster/v3"
)

type fakeIndex map[string]string

func (f fakeIndex) LookupURL(cluster logicalcluster.Name) (string, bool) {
	url, found := f[cluster.String()]
	return url, found
}

func TestWithClusterResolver(t *testing.T) {
	idx := fakeIndex{
		"1abc": "https://shard-a.example.com/services/apiexport/foo/bar",
	}

	var gotShardURL string
	delegate := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := ShardURLFrom(r.Context()); u != nil {
			gotShardURL = u.String()
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := WithClusterResolver(delegate, idx)

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantURL    string
	}{
		{
			name:       "known cluster with trail",
			path:       "/clusters/1abc/apis/foo.example.com/v1/widgets",
			wantStatus: http.StatusOK,
			wantURL:    "https://shard-a.example.com/services/apiexport/foo/bar/clusters/1abc/apis/foo.example.com/v1/widgets",
		},
		{
			name:       "known cluster without trail",
			path:       "/clusters/1abc",
			wantStatus: http.StatusOK,
			wantURL:    "https://shard-a.example.com/services/apiexport/foo/bar/clusters/1abc",
		},
		{
			name:       "unknown cluster",
			path:       "/clusters/unknown/apis",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "wildcard cluster is rejected",
			path:       "/clusters/*/apis",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "unrelated path",
			path:       "/healthz",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotShardURL = ""
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("got status %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantURL != "" && gotShardURL != tc.wantURL {
				t.Fatalf("got shard URL %q, want %q", gotShardURL, tc.wantURL)
			}
		})
	}
}

func TestWithClusterResolverEndpointURLs(t *testing.T) {
	tests := []struct {
		name        string
		endpointURL string
		wantStatus  int
		wantURL     string
	}{
		{
			name:        "endpoint with trailing slash",
			endpointURL: "https://shard-a.example.com/services/apiexport/foo/",
			wantStatus:  http.StatusOK,
			wantURL:     "https://shard-a.example.com/services/apiexport/foo/clusters/1abc/api/v1",
		},
		{
			name:        "endpoint without path",
			endpointURL: "https://shard-a.example.com",
			wantStatus:  http.StatusOK,
			wantURL:     "https://shard-a.example.com/clusters/1abc/api/v1",
		},
		{
			name:        "unparsable endpoint",
			endpointURL: "://not-a-url",
			wantStatus:  http.StatusInternalServerError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotShardURL string
			delegate := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if u := ShardURLFrom(r.Context()); u != nil {
					gotShardURL = u.String()
				}
				w.WriteHeader(http.StatusOK)
			})
			handler := WithClusterResolver(delegate, fakeIndex{"1abc": tc.endpointURL})

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/clusters/1abc/api/v1", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("got status %d, want %d", rec.Code, tc.wantStatus)
			}
			if gotShardURL != tc.wantURL {
				t.Fatalf("got shard URL %q, want %q", gotShardURL, tc.wantURL)
			}
		})
	}
}

func TestShardURLContext(t *testing.T) {
	if u := ShardURLFrom(context.Background()); u != nil {
		t.Fatalf("got %v from an empty context, want nil", u)
	}

	want := &url.URL{Scheme: "https", Host: "shard-a.example.com", Path: "/clusters/1abc"}
	if got := ShardURLFrom(WithShardURL(context.Background(), want)); got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}
