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
		BindAddress: ":8080",
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
	fs.StringVar(&o.TokenFile, "token-file", o.TokenFile,
		"The path to a file containing the bearer token clients must send to use the proxied paths. "+
			"The file is re-read when it changes. Requires --tls-cert-file and --tls-key-file. "+
			"If not set, the proxy performs no authentication.")
	fs.StringVar(&o.TLSCertFile, "tls-cert-file", o.TLSCertFile,
		"The path to the PEM encoded serving certificate. The file is re-read when it changes. "+
			"If not set, the proxy serves plain HTTP.")
	fs.StringVar(&o.TLSKeyFile, "tls-key-file", o.TLSKeyFile,
		"The path to the PEM encoded private key for --tls-cert-file. The file is re-read when it changes.")
}

func (o *Options) Complete() error {
	return nil
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
	if o.TokenFile != "" && o.TLSCertFile == "" {
		errs = append(errs, fmt.Errorf("--token-file requires --tls-cert-file and --tls-key-file"))
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
