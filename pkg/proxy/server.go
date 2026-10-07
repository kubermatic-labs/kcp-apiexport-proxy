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
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kcp-dev/contrib-apiexport-proxy/pkg/proxy/index"
	"github.com/kcp-dev/contrib-apiexport-proxy/pkg/proxy/lookup"
	"github.com/kcp-dev/contrib-apiexport-proxy/pkg/proxy/metrics"
)

type Server struct {
	CompletedConfig
	Handler http.Handler
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
	handler = metrics.WithLatencyTracking(handler)
	handler = withPanicRecovery(handler)

	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
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
		Addr:    s.Options.BindAddress,
		Handler: s.Handler,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("Serving", "address", s.Options.BindAddress)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
