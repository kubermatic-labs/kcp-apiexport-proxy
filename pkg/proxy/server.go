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
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy/accesslog"
	"github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy/auth"
	"github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy/filereload"
	"github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy/index"
	"github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy/lookup"
	"github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy/methods"
	"github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy/metrics"
)

const (
	// readHeaderTimeout limits how long a client may take to send the
	// request headers, so slow clients can't hold connections open.
	readHeaderTimeout = 10 * time.Second

	// idleTimeout limits how long an idle keep-alive connection stays
	// open. There is deliberately no write timeout, as it would cut off
	// long-running watch requests.
	idleTimeout = 120 * time.Second
)

type Server struct {
	CompletedConfig
	Handler http.Handler
	// KeyPair is the serving certificate, or nil if the proxy serves plain
	// HTTP.
	KeyPair *filereload.KeyPair
	// IndexControllers holds one index controller per watched
	// APIExportEndpointSlice, keyed by the slice's name.
	IndexControllers map[string]*index.Controller
}

func NewServer(c CompletedConfig) (*Server, error) {
	s := &Server{
		CompletedConfig: c,
	}

	s.IndexControllers = make(map[string]*index.Controller, len(c.Options.APIExportEndpointSliceNames))
	indexes := make(map[string]index.Index, len(c.Options.APIExportEndpointSliceNames))
	for _, name := range c.Options.APIExportEndpointSliceNames {
		indexController, err := index.NewController(c.IdentityConfig, name)
		if err != nil {
			return nil, fmt.Errorf("failed to create index controller for APIExportEndpointSlice %q: %w", name, err)
		}
		s.IndexControllers[name] = indexController
		indexes[name] = indexController
	}

	transport, err := newTransport(c.IdentityConfig)
	if err != nil {
		return nil, err
	}

	var handler http.Handler = newShardReverseProxy(transport)
	handler = lookup.WithClusterResolver(handler, indexes)
	handler = methods.WithAllowed(handler, c.Options.AllowedHTTPMethods)

	metricsHandler := metrics.Handler()

	if c.Options.TokenFile != "" {
		tokenFile := filereload.New(c.Options.TokenFile)
		if _, _, err := tokenFile.Load(); err != nil {
			return nil, fmt.Errorf("failed to load token: %w", err)
		}
		handler = auth.WithToken(handler, tokenFile)
		metricsHandler = auth.WithToken(metricsHandler, tokenFile)
	}

	if c.Options.TLSCertFile != "" {
		s.KeyPair = filereload.NewKeyPair(c.Options.TLSCertFile, c.Options.TLSKeyFile)
		if _, err := s.KeyPair.GetCertificate(nil); err != nil {
			return nil, fmt.Errorf("failed to load serving certificate: %w", err)
		}
	}
	handler = metrics.WithLatencyTracking(handler)
	handler = withPanicRecovery(handler)
	handler = accesslog.WithLogging(handler)

	mux := http.NewServeMux()
	mux.Handle("/metrics", metricsHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !s.hasSynced() {
			http.Error(w, "index not synced", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("/", handler)
	s.Handler = mux

	return s, nil
}

// hasSynced reports, without blocking, whether the APIExportEndpointSlice
// informers of all index controllers have synced at least once.
func (s *Server) hasSynced() bool {
	for _, indexController := range s.IndexControllers {
		if !indexController.HasSynced() {
			return false
		}
	}
	return true
}

// withPanicRecovery recovers from panics in delegate, logging them and
// returning a 500 instead of crashing the process.
func withPanicRecovery(delegate http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		defer func() {
			if r := recover(); r != nil {
				logger := klog.FromContext(req.Context())
				logger.Error(fmt.Errorf("%v", r), "panic while handling request", "stack", string(debug.Stack()))
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()

		delegate.ServeHTTP(w, req)
	})
}

// preparedServer is a private wrapper that enforces a call of PrepareRun()
// before Run can be invoked.
type preparedServer struct {
	*Server
}

func (s *Server) PrepareRun(context.Context) (preparedServer, error) {
	return preparedServer{s}, nil
}

func (s preparedServer) Run(ctx context.Context) error {
	logger := klog.FromContext(ctx).WithValues("component", "kcp-apiexport-proxy")

	for _, indexController := range s.IndexControllers {
		go indexController.Start(ctx)
	}

	if !cache.WaitForCacheSync(ctx.Done(), s.hasSynced) {
		return fmt.Errorf("failed to sync APIExportEndpointSlice informers")
	}

	httpServer := &http.Server{
		Addr:              s.Options.BindAddress,
		Handler:           s.Handler,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}

	if s.Options.TokenFile != "" && s.KeyPair == nil {
		logger.Info("WARNING: --token-file is set without TLS, so the token and all responses are sent in plain text; " +
			"set --tls-cert-file and --tls-key-file to protect them")
	}

	listenAndServe := httpServer.ListenAndServe
	if s.KeyPair != nil {
		httpServer.TLSConfig = &tls.Config{
			MinVersion:     tls.VersionTLS12,
			GetCertificate: s.KeyPair.GetCertificate,
		}
		listenAndServe = func() error {
			return httpServer.ListenAndServeTLS("", "")
		}
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("Serving", "address", s.Options.BindAddress, "tls", s.KeyPair != nil, "authentication", s.Options.TokenFile != "")
		if err := listenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
