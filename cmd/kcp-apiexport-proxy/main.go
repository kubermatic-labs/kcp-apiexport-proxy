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

package main

import (
	"context"
	goflags "flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"

	"github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy"
	proxyoptions "github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy/options"
	"github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/version"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	klog.InitFlags(nil)
	pflag.CommandLine.AddGoFlagSet(goflags.CommandLine)

	cmd := NewProxyCommand()
	if err := cmd.ExecuteContext(ctx); err != nil {
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	klog.Flush()
}

func NewProxyCommand() *cobra.Command {
	options := proxyoptions.NewOptions()

	cmd := &cobra.Command{
		Use:     "kcp-apiexport-proxy",
		Version: version.Version,
		Short:   "Reverse proxy for kcp APIExports' virtual workspace endpoints",
		Long: `kcp-apiexport-proxy watches the named APIExportEndpointSlices for their shard
virtual workspace URLs, watches APIBindings through each of them to learn
which logical cluster lives behind which shard, and forwards
/apiexportendpointslices/<slice>/clusters/<logical_cluster>/... requests to
the right shard using the identity of a single configured kubeconfig.
Clients can be required to send a bearer token (--token-file), and the
proxy can serve HTTPS (--tls-cert-file, --tls-key-file).`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := options.Complete(); err != nil {
				return err
			}
			if errs := options.Validate(); len(errs) > 0 {
				return utilerrors.NewAggregate(errs)
			}

			config, err := proxy.NewConfig(options)
			if err != nil {
				return err
			}
			completedConfig, err := config.Complete()
			if err != nil {
				return err
			}

			server, err := proxy.NewServer(completedConfig)
			if err != nil {
				return err
			}
			prepared, err := server.PrepareRun(cmd.Context())
			if err != nil {
				return err
			}
			return prepared.Run(cmd.Context())
		},
	}

	options.AddFlags(cmd.Flags())

	return cmd
}
