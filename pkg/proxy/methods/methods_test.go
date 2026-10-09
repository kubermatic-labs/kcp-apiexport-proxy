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

package methods

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithAllowed(t *testing.T) {
	handler := WithAllowed(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), []string{http.MethodGet, http.MethodHead})

	tests := []struct {
		method     string
		wantStatus int
	}{
		{method: http.MethodGet, wantStatus: http.StatusOK},
		{method: http.MethodHead, wantStatus: http.StatusOK},
		{method: http.MethodPost, wantStatus: http.StatusMethodNotAllowed},
		{method: http.MethodDelete, wantStatus: http.StatusMethodNotAllowed},
		{method: "get", wantStatus: http.StatusMethodNotAllowed},
	}

	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), tc.method, "/", nil))

			if rec.Code != tc.wantStatus {
				t.Fatalf("got status %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusMethodNotAllowed {
				if got, want := rec.Header().Get("Allow"), "GET, HEAD"; got != want {
					t.Fatalf("got Allow header %q, want %q", got, want)
				}
			}
		})
	}
}
