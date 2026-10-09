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
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	compbasemetrics "k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/klog/v2"
)

// unknownSlice is the slice label for requests that don't name one of the
// configured APIExportEndpointSlices, which keeps the label's cardinality
// bounded no matter what paths clients send.
const unknownSlice = "unknown"

var requestLatencies = compbasemetrics.NewHistogramVec(
	&compbasemetrics.HistogramOpts{
		Name: "kcp_apiexport_proxy_request_duration_seconds",
		Help: "Response latency distribution in seconds for each verb, HTTP response code and APIExportEndpointSlice.",
		Buckets: []float64{0.05, 0.1, 0.2, 0.4, 0.6, 0.8, 1.0, 1.25, 1.5, 2, 3,
			4, 5, 6, 8, 10, 15, 20, 30, 45, 60},
		StabilityLevel: compbasemetrics.ALPHA,
	},
	[]string{"method", "code", "slice"},
)

var endpoints = compbasemetrics.NewGaugeVec(
	&compbasemetrics.GaugeOpts{
		Name:           "kcp_apiexport_proxy_endpoints",
		Help:           "Number of shard virtual workspace URLs published by each APIExportEndpointSlice.",
		StabilityLevel: compbasemetrics.ALPHA,
	},
	[]string{"slice"},
)

var logicalClusters = compbasemetrics.NewGaugeVec(
	&compbasemetrics.GaugeOpts{
		Name:           "kcp_apiexport_proxy_logical_clusters",
		Help:           "Number of logical clusters known for each APIExportEndpointSlice.",
		StabilityLevel: compbasemetrics.ALPHA,
	},
	[]string{"slice"},
)

var registerMetrics sync.Once

// Register registers the proxy's metrics with the legacy registry. Safe to
// call multiple times.
func Register() {
	registerMetrics.Do(func() {
		legacyregistry.MustRegister(requestLatencies, endpoints, logicalClusters)
	})
}

func init() {
	Register()
}

type sliceContextKey struct{}

// WithLatencyTracking tracks how long the wrapped handler took to complete,
// labelled with the APIExportEndpointSlice named in the request path if it
// is one of slices.
func WithLatencyTracking(delegate http.Handler, slices []string) http.Handler {
	instrumented := promhttp.InstrumentHandlerDuration(requestLatencies.HistogramVec, delegate,
		promhttp.WithLabelFromCtx("slice", func(ctx context.Context) string {
			slice, _ := ctx.Value(sliceContextKey{}).(string)
			return slice
		}),
	)

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := context.WithValue(req.Context(), sliceContextKey{}, sliceLabel(req.URL.Path, slices))
		instrumented.ServeHTTP(w, req.WithContext(ctx))
	})
}

// sliceLabel returns the slice in a "/apiexportendpointslices/<slice>/..."
// path if it is one of known, and unknownSlice otherwise.
func sliceLabel(path string, known []string) string {
	rest, found := strings.CutPrefix(path, "/apiexportendpointslices/")
	if !found {
		return unknownSlice
	}

	slice, _, _ := strings.Cut(rest, "/")
	if !slices.Contains(known, slice) {
		return unknownSlice
	}
	return slice
}

// SetIndexSize records how many shard endpoints and logical clusters the
// index of the given APIExportEndpointSlice currently knows.
func SetIndexSize(slice string, endpointCount, clusterCount int) {
	endpoints.WithLabelValues(slice).Set(float64(endpointCount))
	logicalClusters.WithLabelValues(slice).Set(float64(clusterCount))
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
