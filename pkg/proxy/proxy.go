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
	"fmt"
	"net/http"
	"net/http/httputil"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"

	"github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy/lookup"
	"github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy/metrics"
)

// newTransport builds the single http.RoundTripper used for every outbound
// request to every shard. It presents the identity of identityConfig
// (certificates, bearer token, etc.) regardless of which shard the request
// is ultimately routed to.
func newTransport(identityConfig *rest.Config) (http.RoundTripper, error) {
	transport, err := rest.TransportFor(identityConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport from identity config: %w", err)
	}
	return transport, nil
}

// newShardReverseProxy returns a reverse proxy whose destination is decided
// per-request by lookup.ShardURLFrom.
func newShardReverseProxy(transport http.RoundTripper) *httputil.ReverseProxy {
	rewrite := func(pr *httputil.ProxyRequest) {
		// Rewrite, unlike the deprecated Director, does not add
		// X-Forwarded-For by itself.
		pr.SetXForwarded()

		shardURL := lookup.ShardURLFrom(pr.In.Context())
		if shardURL == nil {
			// should not happen if wiring is correct
			utilruntime.HandleError(fmt.Errorf("no shard URL found in request context"))
			pr.Out.URL.Scheme = "https"
			pr.Out.URL.Host = "notfound"
			return
		}

		pr.Out.URL.Scheme = shardURL.Scheme
		pr.Out.URL.Host = shardURL.Host
		pr.Out.URL.Path = shardURL.Path
	}

	return &httputil.ReverseProxy{
		Rewrite:      rewrite,
		Transport:    transport,
		ErrorHandler: metrics.NewProxyErrorHandler(),
	}
}
