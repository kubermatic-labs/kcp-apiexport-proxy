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
	"testing"

	"github.com/spf13/pflag"
)

func TestNewOptionsDefaults(t *testing.T) {
	o := NewOptions()

	if o.BindAddress != ":8080" {
		t.Fatalf("got bind address %q, want %q", o.BindAddress, ":8080")
	}
	if o.Kubeconfig != "" || o.APIExportEndpointSliceName != "" {
		t.Fatalf("expected kubeconfig and slice name to be empty by default, got %+v", o)
	}
}

func TestAddFlags(t *testing.T) {
	o := NewOptions()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	o.AddFlags(fs)

	err := fs.Parse([]string{
		"--kubeconfig=/tmp/kubeconfig",
		"--apiexportendpointslice-name=my-slice",
		"--bind-address=127.0.0.1:9090",
	})
	if err != nil {
		t.Fatalf("failed to parse flags: %v", err)
	}

	want := Options{
		Kubeconfig:                 "/tmp/kubeconfig",
		APIExportEndpointSliceName: "my-slice",
		BindAddress:                "127.0.0.1:9090",
	}
	if *o != want {
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
		sliceName  string
		wantErrs   int
	}{
		{name: "valid", kubeconfig: "/tmp/kubeconfig", sliceName: "my-slice", wantErrs: 0},
		{name: "missing kubeconfig", sliceName: "my-slice", wantErrs: 1},
		{name: "missing slice name", kubeconfig: "/tmp/kubeconfig", wantErrs: 1},
		{name: "missing both", wantErrs: 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := NewOptions()
			o.Kubeconfig = tc.kubeconfig
			o.APIExportEndpointSliceName = tc.sliceName

			if errs := o.Validate(); len(errs) != tc.wantErrs {
				t.Fatalf("got %d errors (%v), want %d", len(errs), errs, tc.wantErrs)
			}
		})
	}
}
