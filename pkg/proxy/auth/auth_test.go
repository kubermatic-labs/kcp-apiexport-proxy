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

package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy/filereload"
)

func TestWithToken(t *testing.T) {
	tests := []struct {
		name          string
		token         string
		authorization string
		wantStatus    int
	}{
		{name: "valid token", token: "secret\n", authorization: "Bearer secret", wantStatus: http.StatusOK},
		{name: "missing header", token: "secret", wantStatus: http.StatusUnauthorized},
		{name: "wrong token", token: "secret", authorization: "Bearer wrong", wantStatus: http.StatusUnauthorized},
		{name: "wrong scheme", token: "secret", authorization: "Basic secret", wantStatus: http.StatusUnauthorized},
		{name: "empty token never matches", token: " \n", authorization: "Bearer ", wantStatus: http.StatusUnauthorized},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "token")
			if err := os.WriteFile(path, []byte(tc.token), 0o600); err != nil {
				t.Fatalf("failed to write token: %v", err)
			}

			handler := WithToken(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}), filereload.New(path))

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			if tc.authorization != "" {
				req.Header.Set("Authorization", tc.authorization)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("got status %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatalf("expected a WWW-Authenticate header")
			}
		})
	}
}

func TestWithTokenReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("failed to write token: %v", err)
	}

	handler := WithToken(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), filereload.New(path))

	do := func(token string) int {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := do("old"); code != http.StatusOK {
		t.Fatalf("old token: got status %d, want %d", code, http.StatusOK)
	}

	if err := os.WriteFile(path, []byte("rotated"), 0o600); err != nil {
		t.Fatalf("failed to write token: %v", err)
	}

	if code := do("old"); code != http.StatusUnauthorized {
		t.Fatalf("old token after rotation: got status %d, want %d", code, http.StatusUnauthorized)
	}
	if code := do("rotated"); code != http.StatusOK {
		t.Fatalf("rotated token: got status %d, want %d", code, http.StatusOK)
	}
}

func TestWithTokenUnreadableFile(t *testing.T) {
	handler := WithToken(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), filereload.New(filepath.Join(t.TempDir(), "missing")))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
