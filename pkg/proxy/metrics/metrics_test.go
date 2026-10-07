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

// teapotRequestCount returns the number of requests with status 418 recorded
// in the request latency histogram, as exposed by the metrics handler.
func teapotRequestCount(t *testing.T) int {
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

	const prefix = `apiexport_proxy_request_duration_seconds_count{code="418",method="get"} `
	for line := range strings.SplitSeq(string(body), "\n") {
		if value, found := strings.CutPrefix(line, prefix); found {
			count, err := strconv.Atoi(value)
			if err != nil {
				t.Fatalf("failed to parse %q: %v", line, err)
			}
			return count
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
	// Use an unusual status code so the series is unique to this test.
	handler := WithLatencyTracking(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	before := teapotRequestCount(t)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/clusters/1abc", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("got status %d, want %d", rec.Code, http.StatusTeapot)
	}

	if after := teapotRequestCount(t); after != before+1 {
		t.Fatalf("got request count %d, want %d", after, before+1)
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
