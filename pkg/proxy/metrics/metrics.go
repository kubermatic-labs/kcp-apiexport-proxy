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
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	compbasemetrics "k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/klog/v2"
)

var requestLatencies = compbasemetrics.NewHistogramVec(
	&compbasemetrics.HistogramOpts{
		Name: "apiexport_proxy_request_duration_seconds",
		Help: "Response latency distribution in seconds for each verb and HTTP response code.",
		Buckets: []float64{0.05, 0.1, 0.2, 0.4, 0.6, 0.8, 1.0, 1.25, 1.5, 2, 3,
			4, 5, 6, 8, 10, 15, 20, 30, 45, 60},
		StabilityLevel: compbasemetrics.ALPHA,
	},
	[]string{"method", "code"},
)

var registerMetrics sync.Once

// Register registers the proxy's metrics with the legacy registry. Safe to
// call multiple times.
func Register() {
	registerMetrics.Do(func() {
		legacyregistry.MustRegister(requestLatencies)
	})
}

func init() {
	Register()
}

// WithLatencyTracking tracks how long the wrapped handler took to complete.
func WithLatencyTracking(delegate http.Handler) http.Handler {
	return promhttp.InstrumentHandlerDuration(requestLatencies.HistogramVec, delegate)
}

// Handler returns the /metrics HTTP handler.
func Handler() http.Handler {
	return legacyregistry.Handler()
}

// NewProxyErrorHandler returns an error handler for httputil.ReverseProxy
// that logs the error and returns 502 Bad Gateway.
func NewProxyErrorHandler() func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		logger := klog.FromContext(r.Context())
		logger.Error(err, "proxy error")
		w.WriteHeader(http.StatusBadGateway)
	}
}
