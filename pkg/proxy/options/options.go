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

	// BindAddress is the address the plain-HTTP proxy listens on.
	BindAddress string
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
		"The address the proxy listens on. The proxy serves plain HTTP and performs no authentication.")
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
