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
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

	"github.com/kcp-dev/contrib-apiexport-proxy/pkg/proxy/internal/fakekcp"
	proxyoptions "github.com/kcp-dev/contrib-apiexport-proxy/pkg/proxy/options"
)

// newTestFakeKCP returns a fake kcp with a single shard at /shard-a serving
// logical cluster 1abc. Requests to that shard's virtual workspace that the
// fake doesn't serve itself are echoed back with their path.
func newTestFakeKCP(t *testing.T) *fakekcp.Server {
	t.Helper()

	fake := fakekcp.New()
	t.Cleanup(fake.Close)

	fake.AddEndpointSlice("the-slice", "/shard-a")
	fake.AddBinding("/shard-a", "1abc", "binding")
	fake.SetFallback(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.URL.RequestURI())
	}))

	return fake
}

func newTestServer(t *testing.T, fake *fakekcp.Server, bindAddress string) *Server {
	t.Helper()

	opts := proxyoptions.NewOptions()
	opts.APIExportEndpointSliceName = "the-slice"
	opts.BindAddress = bindAddress

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
	if rec := get(t, s.Handler, "/clusters/1abc/api/v1/configmaps"); rec.Code != http.StatusNotFound {
		t.Fatalf("cluster request before sync: got status %d, want %d", rec.Code, http.StatusNotFound)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	go s.IndexController.Start(ctx)

	err := wait.PollUntilContextCancel(ctx, 10*time.Millisecond, true, func(context.Context) (bool, error) {
		_, found := s.IndexController.LookupURL("1abc")
		return found && s.IndexController.HasSynced(), nil
	})
	if err != nil {
		t.Fatalf("index did not converge: %v", err)
	}

	if rec := get(t, s.Handler, "/readyz"); rec.Code != http.StatusOK {
		t.Fatalf("/readyz after sync: got status %d, want %d", rec.Code, http.StatusOK)
	}

	rec := get(t, s.Handler, "/clusters/1abc/api/v1/configmaps?limit=10")
	if rec.Code != http.StatusOK {
		t.Fatalf("cluster request: got status %d, want %d", rec.Code, http.StatusOK)
	}
	if got, want := rec.Body.String(), "/shard-a/clusters/1abc/api/v1/configmaps?limit=10"; got != want {
		t.Fatalf("cluster request: got upstream URI %q, want %q", got, want)
	}

	if rec := get(t, s.Handler, "/clusters/unknown/api/v1/configmaps"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown cluster: got status %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestNewServerInvalidIdentity(t *testing.T) {
	c := &Config{
		Options: proxyoptions.NewOptions(),
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
