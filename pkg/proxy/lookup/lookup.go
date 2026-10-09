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

package lookup

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"k8s.io/klog/v2"

	"github.com/kcp-dev/logicalcluster/v3"

	"github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy/index"
)

// WithClusterResolver resolves the {slice} and {cluster} in
// "/apiexportendpointslices/{slice}/clusters/{cluster}/..." requests against
// the index of the named APIExportEndpointSlice in indexes, storing the
// resolved shard virtual workspace URL in the request context (see
// WithShardURL/ShardURLFrom) before calling delegate. Requests for unknown
// slices or clusters, or that don't match that path, get a 404. Clients are
// authenticated (if at all) before this handler, and there is no per-cluster
// authorization, so there's no "forbidden" framing to fall back to, unlike
// upstream/pkg/proxy/lookup.
func WithClusterResolver(delegate http.Handler, indexes map[string]index.Index) http.Handler {
	mux := http.NewServeMux()

	resolveHandler := newClusterResolveHandler(delegate, indexes)
	mux.HandleFunc("/apiexportendpointslices/{slice}/clusters/{cluster}", resolveHandler)
	mux.HandleFunc("/apiexportendpointslices/{slice}/clusters/{cluster}/{trail...}", resolveHandler)

	return mux
}

func newClusterResolveHandler(delegate http.Handler, indexes map[string]index.Index) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		sliceName := req.PathValue("slice")
		clusterName := req.PathValue("cluster")
		logger := klog.FromContext(req.Context()).WithValues("slice", sliceName, "cluster", clusterName)

		idx, found := indexes[sliceName]
		if !found {
			logger.Info("unknown APIExportEndpointSlice")
			http.NotFound(w, req)
			return
		}

		if clusterName == "" || clusterName == "*" {
			// Only concrete logical cluster names are resolvable; this
			// proxy does not track workspace paths or serve wildcard
			// requests.
			logger.Info("invalid cluster name")
			http.NotFound(w, req)
			return
		}

		endpointURL, found := idx.LookupURL(logicalcluster.Name(clusterName))
		if !found {
			logger.Info("unknown logical cluster")
			http.NotFound(w, req)
			return
		}

		shardURL, err := url.Parse(endpointURL)
		if err != nil {
			logger.Error(err, "failed to parse shard endpoint URL", "url", endpointURL)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		shardURL.Path = strings.TrimSuffix(shardURL.Path, "/") + "/clusters/" + clusterName
		if trail := req.PathValue("trail"); trail != "" {
			shardURL.Path += "/" + trail
		}

		logger.WithValues("to", shardURL).Info("resolved cluster")

		req = req.WithContext(WithShardURL(req.Context(), shardURL))
		delegate.ServeHTTP(w, req)
	}
}

type contextKey int

const shardURLContextKey contextKey = iota

// WithShardURL returns a copy of parent with shardURL attached.
func WithShardURL(parent context.Context, shardURL *url.URL) context.Context {
	return context.WithValue(parent, shardURLContextKey, shardURL)
}

// ShardURLFrom returns the shard URL previously attached with WithShardURL,
// or nil if there is none.
func ShardURLFrom(ctx context.Context) *url.URL {
	shardURL, ok := ctx.Value(shardURLContextKey).(*url.URL)
	if !ok {
		return nil
	}
	return shardURL
}
