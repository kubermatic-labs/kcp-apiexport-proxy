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

package metrics

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// scrape returns the value of the metric series whose text exposition line
// starts with series (name plus labels), or 0 if there is none.
func scrape(t *testing.T, series string) float64 {
	t.Helper()

	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d from the metrics handler, want %d", rec.Code, http.StatusOK)
	}

	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("failed to read metrics: %v", err)
	}

	for line := range strings.SplitSeq(string(body), "\n") {
		if value, found := strings.CutPrefix(line, series+" "); found {
			f, err := strconv.ParseFloat(value, 64)
			if err != nil {
				t.Fatalf("failed to parse %q: %v", line, err)
			}
			return f
		}
	}

	return 0
}

func TestRegisterIsIdempotent(t *testing.T) {
	// init() already registered the metrics once; registering again must
	// not panic with a duplicate registration.
	Register()
	Register()
}

func TestWithLatencyTracking(t *testing.T) {
	// Use an unusual status code so the series are unique to this test.
	handler := WithLatencyTracking(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}), []string{"slice-a"})

	tests := []struct {
		path      string
		wantSlice string
	}{
		{path: "/apiexportendpointslices/slice-a/clusters/1abc", wantSlice: "slice-a"},
		{path: "/apiexportendpointslices/not-configured/clusters/1abc", wantSlice: "unknown"},
		{path: "/something/else", wantSlice: "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			series := `kcp_apiexport_proxy_request_duration_seconds_count{code="418",method="get",slice="` + tc.wantSlice + `"}`
			before := scrape(t, series)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil))
			if rec.Code != http.StatusTeapot {
				t.Fatalf("got status %d, want %d", rec.Code, http.StatusTeapot)
			}

			if after := scrape(t, series); after != before+1 {
				t.Fatalf("got request count %v, want %v", after, before+1)
			}
		})
	}
}

func TestSetIndexSize(t *testing.T) {
	SetIndexSize("index-test", 2, 5)

	if got := scrape(t, `kcp_apiexport_proxy_endpoints{slice="index-test"}`); got != 2 {
		t.Fatalf("got %v endpoints, want 2", got)
	}
	if got := scrape(t, `kcp_apiexport_proxy_logical_clusters{slice="index-test"}`); got != 5 {
		t.Fatalf("got %v logical clusters, want 5", got)
	}
}

func TestNewProxyErrorHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/clusters/1abc", nil)

	NewProxyErrorHandler()(rec, req, errors.New("shard unreachable"))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("got status %d, want %d", rec.Code, http.StatusBadGateway)
	}
}
