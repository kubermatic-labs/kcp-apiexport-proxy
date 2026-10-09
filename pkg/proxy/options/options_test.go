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
	"reflect"
	"testing"

	"github.com/spf13/pflag"
)

func TestNewOptionsDefaults(t *testing.T) {
	o := NewOptions()

	if o.BindAddress != ":8080" {
		t.Fatalf("got bind address %q, want %q", o.BindAddress, ":8080")
	}
	if o.Kubeconfig != "" || len(o.APIExportEndpointSliceNames) != 0 {
		t.Fatalf("expected kubeconfig and slice names to be empty by default, got %+v", o)
	}
}

func TestAddFlags(t *testing.T) {
	o := NewOptions()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	o.AddFlags(fs)

	err := fs.Parse([]string{
		"--kubeconfig=/tmp/kubeconfig",
		"--apiexportendpointslice-names=slice-a,slice-b",
		"--bind-address=127.0.0.1:9090",
		"--token-file=/tmp/token",
		"--tls-cert-file=/tmp/tls.crt",
		"--tls-key-file=/tmp/tls.key",
	})
	if err != nil {
		t.Fatalf("failed to parse flags: %v", err)
	}

	want := Options{
		Kubeconfig:                  "/tmp/kubeconfig",
		APIExportEndpointSliceNames: []string{"slice-a", "slice-b"},
		BindAddress:                 "127.0.0.1:9090",
		TokenFile:                   "/tmp/token",
		TLSCertFile:                 "/tmp/tls.crt",
		TLSKeyFile:                  "/tmp/tls.key",
	}
	if !reflect.DeepEqual(*o, want) {
		t.Fatalf("got %+v, want %+v", *o, want)
	}
}

func TestComplete(t *testing.T) {
	if err := NewOptions().Complete(); err != nil {
		t.Fatalf("Complete: %v", err)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name       string
		kubeconfig string
		sliceNames []string
		tokenFile  string
		certFile   string
		keyFile    string
		wantErrs   int
	}{
		{name: "valid", kubeconfig: "/tmp/kubeconfig", sliceNames: []string{"my-slice"}, wantErrs: 0},
		{name: "valid with multiple slices", kubeconfig: "/tmp/kubeconfig", sliceNames: []string{"slice-a", "slice-b"}, wantErrs: 0},
		{name: "missing kubeconfig", sliceNames: []string{"my-slice"}, wantErrs: 1},
		{name: "missing slice names", kubeconfig: "/tmp/kubeconfig", wantErrs: 1},
		{name: "missing both", wantErrs: 2},
		{name: "empty slice name", kubeconfig: "/tmp/kubeconfig", sliceNames: []string{"slice-a", ""}, wantErrs: 1},
		{name: "duplicate slice name", kubeconfig: "/tmp/kubeconfig", sliceNames: []string{"slice-a", "slice-a"}, wantErrs: 1},
		{name: "TLS", kubeconfig: "/tmp/kubeconfig", sliceNames: []string{"my-slice"}, certFile: "/tmp/tls.crt", keyFile: "/tmp/tls.key", wantErrs: 0},
		{name: "TLS and token", kubeconfig: "/tmp/kubeconfig", sliceNames: []string{"my-slice"}, tokenFile: "/tmp/token", certFile: "/tmp/tls.crt", keyFile: "/tmp/tls.key", wantErrs: 0},
		{name: "TLS certificate without key", kubeconfig: "/tmp/kubeconfig", sliceNames: []string{"my-slice"}, certFile: "/tmp/tls.crt", wantErrs: 1},
		{name: "TLS key without certificate", kubeconfig: "/tmp/kubeconfig", sliceNames: []string{"my-slice"}, keyFile: "/tmp/tls.key", wantErrs: 1},
		{name: "token without TLS", kubeconfig: "/tmp/kubeconfig", sliceNames: []string{"my-slice"}, tokenFile: "/tmp/token", wantErrs: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := NewOptions()
			o.Kubeconfig = tc.kubeconfig
			o.APIExportEndpointSliceNames = tc.sliceNames
			o.TokenFile = tc.tokenFile
			o.TLSCertFile = tc.certFile
			o.TLSKeyFile = tc.keyFile

			if errs := o.Validate(); len(errs) != tc.wantErrs {
				t.Fatalf("got %d errors (%v), want %d", len(errs), errs, tc.wantErrs)
			}
		})
	}
}
