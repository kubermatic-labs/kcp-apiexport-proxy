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
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

	"github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy/internal/fakekcp"
	"github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy/internal/testcerts"
	proxyoptions "github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy/options"
)

// newTestFakeKCP returns a fake kcp with two APIExportEndpointSlices: slice-a
// with a shard at /shard-a serving logical cluster 1abc, and slice-b with a
// shard at /shard-b serving logical cluster 2def. Requests to the shards'
// virtual workspaces that the fake doesn't serve itself are echoed back with
// their path.
func newTestFakeKCP(t *testing.T) *fakekcp.Server {
	t.Helper()

	fake := fakekcp.New()
	t.Cleanup(fake.Close)

	fake.AddEndpointSlice("slice-a", "/shard-a")
	fake.AddBinding("/shard-a", "1abc", "binding")
	fake.AddEndpointSlice("slice-b", "/shard-b")
	fake.AddBinding("/shard-b", "2def", "binding")
	fake.SetFallback(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.URL.RequestURI())
	}))

	return fake
}

func newTestServer(t *testing.T, fake *fakekcp.Server, bindAddress string, configure ...func(*proxyoptions.Options)) *Server {
	t.Helper()

	opts := proxyoptions.NewOptions()
	opts.APIExportEndpointSliceNames = []string{"slice-a", "slice-b"}
	opts.BindAddress = bindAddress
	for _, f := range configure {
		f(opts)
	}

	c := &Config{Options: opts, ExtraConfig: ExtraConfig{IdentityConfig: &rest.Config{Host: fake.URL}}}
	completed, err := c.Complete()
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	s, err := NewServer(completed)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return s
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
	return rec
}

func TestNewServerHandler(t *testing.T) {
	fake := newTestFakeKCP(t)
	s := newTestServer(t, fake, "")

	if rec := get(t, s.Handler, "/healthz"); rec.Code != http.StatusOK {
		t.Fatalf("/healthz: got status %d, want %d", rec.Code, http.StatusOK)
	}
	if rec := get(t, s.Handler, "/readyz"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz before sync: got status %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if rec := get(t, s.Handler, "/metrics"); rec.Code != http.StatusOK {
		t.Fatalf("/metrics: got status %d, want %d", rec.Code, http.StatusOK)
	}
	if rec := get(t, s.Handler, "/apiexportendpointslices/slice-a/clusters/1abc/api/v1/configmaps"); rec.Code != http.StatusNotFound {
		t.Fatalf("cluster request before sync: got status %d, want %d", rec.Code, http.StatusNotFound)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for _, indexController := range s.IndexControllers {
		go indexController.Start(ctx)
	}

	err := wait.PollUntilContextCancel(ctx, 10*time.Millisecond, true, func(context.Context) (bool, error) {
		_, foundA := s.IndexControllers["slice-a"].LookupURL("1abc")
		_, foundB := s.IndexControllers["slice-b"].LookupURL("2def")
		return foundA && foundB && s.hasSynced(), nil
	})
	if err != nil {
		t.Fatalf("index did not converge: %v", err)
	}

	if rec := get(t, s.Handler, "/readyz"); rec.Code != http.StatusOK {
		t.Fatalf("/readyz after sync: got status %d, want %d", rec.Code, http.StatusOK)
	}

	tests := []struct {
		path    string
		wantURI string
	}{
		{
			path:    "/apiexportendpointslices/slice-a/clusters/1abc/api/v1/configmaps?limit=10",
			wantURI: "/shard-a/clusters/1abc/api/v1/configmaps?limit=10",
		},
		{
			path:    "/apiexportendpointslices/slice-b/clusters/2def/api/v1/configmaps",
			wantURI: "/shard-b/clusters/2def/api/v1/configmaps",
		},
	}
	for _, tc := range tests {
		rec := get(t, s.Handler, tc.path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: got status %d, want %d", tc.path, rec.Code, http.StatusOK)
		}
		if got := rec.Body.String(); got != tc.wantURI {
			t.Fatalf("%s: got upstream URI %q, want %q", tc.path, got, tc.wantURI)
		}
	}

	rec := httptest.NewRecorder()
	s.Handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/apiexportendpointslices/slice-a/clusters/1abc/api/v1/configmaps", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: got status %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}

	for _, path := range []string{
		"/apiexportendpointslices/slice-a/clusters/unknown/api/v1/configmaps",
		"/apiexportendpointslices/slice-a/clusters/2def/api/v1/configmaps",
		"/apiexportendpointslices/unknown/clusters/1abc/api/v1/configmaps",
	} {
		if rec := get(t, s.Handler, path); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: got status %d, want %d", path, rec.Code, http.StatusNotFound)
		}
	}
}

func TestNewServerInvalidIdentity(t *testing.T) {
	opts := proxyoptions.NewOptions()
	opts.APIExportEndpointSliceNames = []string{"slice-a"}

	c := &Config{
		Options: opts,
		ExtraConfig: ExtraConfig{IdentityConfig: &rest.Config{
			Host:            "https://kcp.example.com",
			TLSClientConfig: rest.TLSClientConfig{CAData: []byte("not a certificate")},
		}},
	}
	completed, err := c.Complete()
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if _, err := NewServer(completed); err == nil {
		t.Fatalf("expected an error for an invalid identity config")
	}
}

func TestWithPanicRecovery(t *testing.T) {
	panicking := withPanicRecovery(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	if rec := get(t, panicking, "/"); rec.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	ok := withPanicRecovery(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	if rec := get(t, ok, "/"); rec.Code != http.StatusNoContent {
		t.Fatalf("got status %d, want %d", rec.Code, http.StatusNoContent)
	}
}

// freeAddress returns a local address that was free at the time of the call.
func freeAddress(t *testing.T) string {
	t.Helper()

	l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find a free port: %v", err)
	}
	defer func() { _ = l.Close() }()

	return l.Addr().String()
}

func TestRun(t *testing.T) {
	fake := newTestFakeKCP(t)
	addr := freeAddress(t)
	s := newTestServer(t, fake, addr)

	prepared, err := s.PrepareRun(t.Context())
	if err != nil {
		t.Fatalf("PrepareRun: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	runErr := make(chan error, 1)
	go func() {
		runErr <- prepared.Run(ctx)
	}()

	pollCtx, pollCancel := context.WithTimeout(ctx, 10*time.Second)
	defer pollCancel()
	err = wait.PollUntilContextCancel(pollCtx, 10*time.Millisecond, true, func(ctx context.Context) (bool, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/readyz", nil)
		if err != nil {
			return false, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false, nil
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK, nil
	})
	if err != nil {
		t.Fatalf("server did not become ready: %v", err)
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned an error after shutdown: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("Run did not return after the context was cancelled")
	}
}

func TestRunListenError(t *testing.T) {
	fake := newTestFakeKCP(t)

	// Keep the address occupied so ListenAndServe fails.
	l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer func() { _ = l.Close() }()

	s := newTestServer(t, fake, l.Addr().String())
	prepared, err := s.PrepareRun(t.Context())
	if err != nil {
		t.Fatalf("PrepareRun: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := prepared.Run(ctx); err == nil {
		t.Fatalf("expected Run to fail when the bind address is in use")
	}
}

func TestRunCacheSyncFailure(t *testing.T) {
	fake := newTestFakeKCP(t)
	s := newTestServer(t, fake, "")

	prepared, err := s.PrepareRun(t.Context())
	if err != nil {
		t.Fatalf("PrepareRun: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := prepared.Run(ctx); err == nil {
		t.Fatalf("expected Run to fail when the context is cancelled before the cache syncs")
	}
}

// writeSecrets writes a serving certificate, its key and a token into a
// temporary directory and returns a function that configures the options to
// use them, plus the certificate.
func writeSecrets(t *testing.T, token string) (func(*proxyoptions.Options), []byte) {
	t.Helper()

	dir := t.TempDir()
	certPEM, keyPEM := testcerts.Generate(t)

	files := map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM, "token": []byte(token)}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	return func(o *proxyoptions.Options) {
		o.TLSCertFile = filepath.Join(dir, "tls.crt")
		o.TLSKeyFile = filepath.Join(dir, "tls.key")
		o.TokenFile = filepath.Join(dir, "token")
	}, certPEM
}

func TestRunWithTLSAndToken(t *testing.T) {
	fake := newTestFakeKCP(t)
	fake.SetFallback(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "authorization="+r.Header.Get("Authorization"))
	}))

	configure, certPEM := writeSecrets(t, "secret\n")
	addr := freeAddress(t)
	s := newTestServer(t, fake, addr, configure)

	prepared, err := s.PrepareRun(t.Context())
	if err != nil {
		t.Fatalf("PrepareRun: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		_ = prepared.Run(ctx)
	}()

	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(certPEM)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}

	do := func(ctx context.Context, path, token string) (int, string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+addr+path, nil)
		if err != nil {
			return 0, "", err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, "", err
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body), err
	}

	pollCtx, pollCancel := context.WithTimeout(ctx, 10*time.Second)
	defer pollCancel()
	err = wait.PollUntilContextCancel(pollCtx, 10*time.Millisecond, true, func(ctx context.Context) (bool, error) {
		code, _, err := do(ctx, "/readyz", "")
		return err == nil && code == http.StatusOK, nil
	})
	if err != nil {
		t.Fatalf("server did not become ready over TLS without a token: %v", err)
	}

	const path = "/apiexportendpointslices/slice-a/clusters/1abc/api/v1/configmaps"

	err = wait.PollUntilContextCancel(pollCtx, 10*time.Millisecond, true, func(ctx context.Context) (bool, error) {
		code, _, err := do(ctx, path, "secret")
		return err == nil && code == http.StatusOK, nil
	})
	if err != nil {
		t.Fatalf("proxied request with the token did not succeed: %v", err)
	}

	code, body, err := do(t.Context(), path, "secret")
	if err != nil {
		t.Fatalf("request with token: %v", err)
	}
	if code != http.StatusOK || body != "authorization=" {
		t.Fatalf("request with token: got status %d and body %q, want %d and the token not forwarded", code, body, http.StatusOK)
	}

	for _, token := range []string{"", "wrong"} {
		code, _, err := do(t.Context(), path, token)
		if err != nil {
			t.Fatalf("request with token %q: %v", token, err)
		}
		if code != http.StatusUnauthorized {
			t.Fatalf("request with token %q: got status %d, want %d", token, code, http.StatusUnauthorized)
		}
	}

	code, _, err = do(t.Context(), "/apiexportendpointslices/slice-a/clusters/unknown/api/v1/configmaps", "")
	if err != nil {
		t.Fatalf("request for unknown cluster without token: %v", err)
	}
	if code != http.StatusUnauthorized {
		t.Fatalf("request for unknown cluster without token: got status %d, want %d", code, http.StatusUnauthorized)
	}
}

func TestNewServerInvalidSecrets(t *testing.T) {
	fake := newTestFakeKCP(t)

	tests := []struct {
		name      string
		configure func(*proxyoptions.Options)
	}{
		{
			name: "missing token file",
			configure: func(o *proxyoptions.Options) {
				o.TokenFile = filepath.Join(t.TempDir(), "missing")
			},
		},
		{
			name: "missing certificate",
			configure: func(o *proxyoptions.Options) {
				o.TLSCertFile = filepath.Join(t.TempDir(), "tls.crt")
				o.TLSKeyFile = filepath.Join(t.TempDir(), "tls.key")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := proxyoptions.NewOptions()
			opts.APIExportEndpointSliceNames = []string{"slice-a"}
			tc.configure(opts)

			c := &Config{Options: opts, ExtraConfig: ExtraConfig{IdentityConfig: &rest.Config{Host: fake.URL}}}
			completed, err := c.Complete()
			if err != nil {
				t.Fatalf("Complete: %v", err)
			}

			if _, err := NewServer(completed); err == nil {
				t.Fatalf("expected an error")
			}
		})
	}
}
