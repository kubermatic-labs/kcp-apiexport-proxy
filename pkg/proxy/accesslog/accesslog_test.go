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

package accesslog

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr/funcr"

	"k8s.io/klog/v2"
)

// serve sends a request through WithLogging(delegate) and returns the
// response and the log lines written for it.
func serve(t *testing.T, delegate http.Handler, req *http.Request) (*httptest.ResponseRecorder, []string) {
	t.Helper()

	var lines []string
	logger := funcr.New(func(prefix, args string) {
		lines = append(lines, prefix+args)
	}, funcr.Options{})

	rec := httptest.NewRecorder()
	WithLogging(delegate).ServeHTTP(rec, req.WithContext(klog.NewContext(req.Context(), logger)))

	return rec, lines
}

func TestWithLogging(t *testing.T) {
	tests := []struct {
		name       string
		delegate   http.Handler
		wantStatus int
	}{
		{
			name: "explicit status",
			delegate: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
			}),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "implicit status from a write",
			delegate: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("ok"))
			}),
			wantStatus: http.StatusOK,
		},
		{
			name:       "nothing written",
			delegate:   http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/apiexportendpointslices/a/clusters/1abc/api?limit=1", nil)
			req.Header.Set("Authorization", "Bearer secret-token")
			req.Header.Set("User-Agent", "test-agent")

			rec, lines := serve(t, tc.delegate, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("got response status %d, want %d", rec.Code, tc.wantStatus)
			}
			if len(lines) != 1 {
				t.Fatalf("got %d log lines, want 1: %v", len(lines), lines)
			}

			line := lines[0]
			for _, want := range []string{
				`"method"="GET"`,
				`"path"="/apiexportendpointslices/a/clusters/1abc/api"`,
				fmt.Sprintf(`"status"=%d`, tc.wantStatus),
				`"userAgent"="test-agent"`,
				`"remoteAddr"=`,
				`"duration"=`,
			} {
				if !strings.Contains(line, want) {
					t.Fatalf("log line %q does not contain %s", line, want)
				}
			}
			if strings.Contains(line, "secret-token") {
				t.Fatalf("log line %q contains the token", line)
			}
		})
	}
}

func TestWithLoggingFlush(t *testing.T) {
	delegate := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("Flush: %v", err)
		}
	})

	rec, _ := serve(t, delegate, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	if !rec.Flushed {
		t.Fatalf("expected the response to be flushed")
	}
}
