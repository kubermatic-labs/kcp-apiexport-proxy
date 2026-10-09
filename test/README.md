# Integration test

`make test-integration` runs [test-integration.sh](test-integration.sh),
which checks the proxy end to end in a throwaway [kind](https://kind.sigs.k8s.io/)
cluster. It needs Docker and curl; kind, kubectl, Helm and Kyverno are used at
the versions pinned in [hack/lib.sh](../hack/lib.sh).

## What it does

1. Builds the proxy and a `kcp-apiexport-proxy:test` image from
   [build/Dockerfile](../build/Dockerfile), creates the kind cluster and loads
   the image into it.
2. Installs Kyverno and deploys a single-shard kcp
   ([manifests/cluster/kcp.yaml](manifests/cluster/kcp.yaml)).
3. Sets up kcp ([manifests/kcp/](manifests/kcp/)):
   - a `provider` workspace with an `example.com` APIExport for a `Widget`
     resource; kcp creates the matching APIExportEndpointSlice,
   - a `consumer` workspace that binds the APIExport and contains a `Widget`.
4. Installs the proxy with its Helm chart
   ([deploy/charts/kcp-apiexport-proxy](../deploy/charts/kcp-apiexport-proxy),
   values in [manifests/cluster/proxy-values.yaml](manifests/cluster/proxy-values.yaml)),
   using the chart's defaults: a generated token and plain HTTP.
5. Through a port-forward to the proxy, checks that:
   - requests with the token return the consumer's `Widget`s and APIBindings,
   - requests for an unknown logical cluster get a `404`,
   - requests without a token, or with a wrong one, get a `401`.
6. Applies a Kyverno `ValidatingPolicy`
   ([manifests/cluster/policy.yaml](manifests/cluster/policy.yaml)) that
   reads the token from the chart's Secret
   ([manifests/cluster/kyverno-rbac.yaml](manifests/cluster/kyverno-rbac.yaml)
   allows that) and asks the proxy whether the logical cluster in a
   ConfigMap's `kcp.io/cluster` annotation has any APIBindings. It then checks
   that a ConfigMap for an unknown cluster is denied and one for the
   consumer's cluster is admitted.

The cluster is deleted afterwards. Set `KEEP_CLUSTER=true` to keep it; its
kubeconfig is then in `_output/kind-kcp-apiexport-proxy.kubeconfig`.

## Why the test does not use TLS

The chart can serve HTTPS with a certificate from cert-manager
(`tls.enabled=true`), but a Kyverno policy currently cannot call a server
whose certificate is signed by a custom CA:

- Kyverno's CEL `http` library offers `http.Client(caBundle)` for this, but
  the function returns the new client without wrapping it in the `http.Context`
  type that `Get` and `Post` are declared on
  ([`http_client_string`](https://github.com/kyverno/sdk/blob/68d74afcb07a4ad8c941e8bc43e9da31d9f703b0/extensions/cel/libs/http/impl.go#L70-L82)
  in the Kyverno SDK).
- As a result `http.Client(ca).Get(...)` fails at admission time with
  `no such overload: Get(libs.mockAwareHTTPContext, string, map)`
  (`mockAwareHTTPContext` is Kyverno's wrapper in
  [pkg/cel/libs/http_mock.go](https://github.com/kyverno/kyverno/blob/v1.19.1/pkg/cel/libs/http_mock.go)).
- Plain `http.Get("https://...")` only trusts Kyverno's system CA roots and
  has no option to skip verification.

This is not fixed in Kyverno v1.19.1, the latest release at the time of
writing, nor on the SDK's main branch. Until it is, the chart defaults to
plain HTTP, and the token is sent unencrypted inside the cluster; the proxy
logs a warning about this at startup.
