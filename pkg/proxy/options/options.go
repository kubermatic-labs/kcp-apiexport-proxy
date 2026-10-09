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

package options

import (
	"fmt"
	"net/http"
	"slices"

	"github.com/spf13/pflag"
)

// Options holds the configuration for the APIExport virtual workspace proxy.
type Options struct {
	// Kubeconfig is the path to the kubeconfig whose current context
	// supplies both the home cluster (where the named APIExportEndpointSlice
	// lives) and the identity used for every outbound request to shards.
	Kubeconfig string

	// APIExportEndpointSliceNames are the names of the
	// APIExportEndpointSlices to watch. They all live in the home cluster.
	APIExportEndpointSliceNames []string

	// BindAddress is the address the proxy listens on.
	BindAddress string

	// AllowedHTTPMethods are the HTTP methods the proxy accepts for proxied
	// requests. They must be among the methods the Kubernetes API uses,
	// spelled exactly (e.g. "GET").
	AllowedHTTPMethods []string

	// TokenFile is the path to a file containing the bearer token clients
	// must send. If empty, the proxy performs no authentication.
	TokenFile string

	// TLSCertFile and TLSKeyFile are the paths to the PEM encoded serving
	// certificate and key. If empty, the proxy serves plain HTTP.
	TLSCertFile string
	TLSKeyFile  string
}

func NewOptions() *Options {
	return &Options{
		BindAddress:        ":8080",
		AllowedHTTPMethods: []string{"GET"},
	}
}

func (o *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Kubeconfig, "kubeconfig", o.Kubeconfig,
		"The path to the kubeconfig used both to locate the APIExportEndpointSlice (via its current "+
			"context) and as the identity forwarded to every shard.")
	fs.StringSliceVar(&o.APIExportEndpointSliceNames, "apiexportendpointslice-names", o.APIExportEndpointSliceNames,
		"Comma-separated names of the APIExportEndpointSlices to watch for shard virtual workspace URLs. "+
			"Requests are proxied under /apiexportendpointslices/<name>/clusters/<logical_cluster>/....")
	fs.StringVar(&o.BindAddress, "bind-address", o.BindAddress,
		"The address the proxy listens on.")
	fs.StringSliceVar(&o.AllowedHTTPMethods, "allowed-http-methods", o.AllowedHTTPMethods,
		"Comma-separated HTTP methods the proxy accepts for proxied requests; others get a 405. "+
			"Must be among GET, POST, PUT, PATCH and DELETE, in uppercase.")
	fs.StringVar(&o.TokenFile, "token-file", o.TokenFile,
		"The path to a file containing the bearer token clients must send to use the proxied paths. "+
			"The file is re-read when it changes. Use it together with --tls-cert-file and --tls-key-file, "+
			"otherwise the token is sent in plain text. If not set, the proxy performs no authentication.")
	fs.StringVar(&o.TLSCertFile, "tls-cert-file", o.TLSCertFile,
		"The path to the PEM encoded serving certificate. The file is re-read when it changes. "+
			"If not set, the proxy serves plain HTTP.")
	fs.StringVar(&o.TLSKeyFile, "tls-key-file", o.TLSKeyFile,
		"The path to the PEM encoded private key for --tls-cert-file. The file is re-read when it changes.")
}

func (o *Options) Complete() error {
	return nil
}

// knownHTTPMethods are the HTTP methods the Kubernetes API uses.
var knownHTTPMethods = []string{
	http.MethodGet,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
}

func (o *Options) Validate() []error {
	var errs []error

	if o.Kubeconfig == "" {
		errs = append(errs, fmt.Errorf("--kubeconfig is required"))
	}
	if len(o.APIExportEndpointSliceNames) == 0 {
		errs = append(errs, fmt.Errorf("--apiexportendpointslice-names is required"))
	}

	if (o.TLSCertFile == "") != (o.TLSKeyFile == "") {
		errs = append(errs, fmt.Errorf("--tls-cert-file and --tls-key-file must be set together"))
	}

	if len(o.AllowedHTTPMethods) == 0 {
		errs = append(errs, fmt.Errorf("--allowed-http-methods must not be empty"))
	}

	seenMethods := make(map[string]bool, len(o.AllowedHTTPMethods))
	for _, method := range o.AllowedHTTPMethods {
		switch {
		case !slices.Contains(knownHTTPMethods, method):
			errs = append(errs, fmt.Errorf("--allowed-http-methods contains unknown method %q, must be one of %v", method, knownHTTPMethods))
		case seenMethods[method]:
			errs = append(errs, fmt.Errorf("--allowed-http-methods contains %q more than once", method))
		}
		seenMethods[method] = true
	}

	seen := make(map[string]bool, len(o.APIExportEndpointSliceNames))
	for _, name := range o.APIExportEndpointSliceNames {
		switch {
		case name == "":
			errs = append(errs, fmt.Errorf("--apiexportendpointslice-names must not contain empty names"))
		case seen[name]:
			errs = append(errs, fmt.Errorf("--apiexportendpointslice-names contains %q more than once", name))
		}
		seen[name] = true
	}

	return errs
}
