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

	"k8s.io/client-go/tools/clientcmd"

	proxyoptions "github.com/kcp-dev/contrib-apiexport-proxy/pkg/proxy/options"

	"k8s.io/client-go/rest"
)

type Config struct {
	Options *proxyoptions.Options

	ExtraConfig
}

type completedConfig struct {
	Options *proxyoptions.Options

	ExtraConfig
}

// ExtraConfig holds configuration that isn't a direct CLI flag.
type ExtraConfig struct {
	// IdentityConfig is loaded from the configured kubeconfig's current
	// context. It is used both to reach the home cluster (where the
	// watched APIExportEndpointSlice lives) and, with its Host swapped per
	// request, as the identity presented to every shard.
	IdentityConfig *rest.Config
}

// CompletedConfig embeds a private pointer that cannot be instantiated
// outside of this package.
type CompletedConfig struct {
	*completedConfig
}

// Complete fills in any fields not set that are required to have valid
// data. It's mutating the receiver.
func (c *Config) Complete() (CompletedConfig, error) {
	return CompletedConfig{&completedConfig{
		Options:     c.Options,
		ExtraConfig: c.ExtraConfig,
	}}, nil
}

// NewConfig returns a new Config for the given options.
func NewConfig(opts *proxyoptions.Options) (*Config, error) {
	c := &Config{
		Options: opts,
	}

	identityConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: opts.Kubeconfig},
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig %q: %w", opts.Kubeconfig, err)
	}
	c.IdentityConfig = identityConfig

	return c, nil
}
