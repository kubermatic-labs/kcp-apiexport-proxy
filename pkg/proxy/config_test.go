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
	"os"
	"path/filepath"
	"testing"

	"k8s.io/client-go/rest"

	proxyoptions "github.com/kcp-dev/contrib-apiexport-proxy/pkg/proxy/options"
)

const testKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: kcp
  cluster:
    server: https://kcp.example.com/clusters/root:provider
contexts:
- name: kcp
  context:
    cluster: kcp
    user: admin
current-context: kcp
users:
- name: admin
  user:
    token: secret-token
`

func TestNewConfig(t *testing.T) {
	kubeconfig := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte(testKubeconfig), 0o600); err != nil {
		t.Fatalf("failed to write kubeconfig: %v", err)
	}

	opts := proxyoptions.NewOptions()
	opts.Kubeconfig = kubeconfig

	c, err := NewConfig(opts)
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}

	if c.Options != opts {
		t.Fatalf("expected options to be kept")
	}
	if got, want := c.IdentityConfig.Host, "https://kcp.example.com/clusters/root:provider"; got != want {
		t.Fatalf("got host %q, want %q", got, want)
	}
	if got, want := c.IdentityConfig.BearerToken, "secret-token"; got != want {
		t.Fatalf("got bearer token %q, want %q", got, want)
	}
}

func TestNewConfigMissingKubeconfig(t *testing.T) {
	opts := proxyoptions.NewOptions()
	opts.Kubeconfig = filepath.Join(t.TempDir(), "does-not-exist")

	if _, err := NewConfig(opts); err == nil {
		t.Fatalf("expected an error for a missing kubeconfig")
	}
}

func TestComplete(t *testing.T) {
	opts := proxyoptions.NewOptions()
	identity := &rest.Config{Host: "https://kcp.example.com"}

	c := &Config{Options: opts, ExtraConfig: ExtraConfig{IdentityConfig: identity}}
	completed, err := c.Complete()
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if completed.Options != opts {
		t.Fatalf("expected options to be carried over")
	}
	if completed.IdentityConfig != identity {
		t.Fatalf("expected identity config to be carried over")
	}
}
