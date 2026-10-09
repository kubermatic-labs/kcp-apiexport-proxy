# kcp-apiexport-proxy

`kcp-apiexport-proxy` is a small reverse proxy that sits in front of the
shard-specific virtual workspace endpoints of one or more kcp `APIExports`.
It gives clients (for example Kyverno policies using the
[HTTP CEL library](https://kyverno.io/docs/policy-types/cel-libraries/)) one
stable URL through which they can reach any logical cluster bound to those
`APIExports`, regardless of which shard it lives on.

## How it works

1. Watches each configured `APIExportEndpointSlice` for its set of per-shard
   virtual workspace URLs.
2. Watches `APIBindings` through each of those URLs and builds, per slice, an
   index from logical cluster name to virtual workspace URL.
3. Forwards requests of the form
   `/apiexportendpointslices/<slice>/clusters/<logical-cluster>/...` to the
   right shard, using the identity from the configured kubeconfig. Requests
   for unknown slices or logical clusters get a `404`.

By default the proxy serves plain HTTP and performs **no authentication**.
With `--token-file`, clients must send `Authorization: Bearer <token>` to use
the proxied paths, and with `--tls-cert-file` and `--tls-key-file` the proxy
serves HTTPS; a token requires TLS. All three files are reloaded when they
change, for example when they are mounted from Secrets. The proxy never
forwards a client's `Authorization` or `Impersonate-*` headers to kcp.

## Development

| Target | Description |
| --- | --- |
| `make build` | Builds the binaries in `cmd/` with the local Go toolchain into `_output/`, plus a `.tar.gz` archive of each binary and its `.sha256` checksum. |
| `make test` | Runs all unit tests. |
| `make test-integration` | Runs [test/test-integration.sh](test/test-integration.sh), which deploys kcp, Kyverno and the proxy into a kind cluster and checks the proxy end to end. Needs Docker. |
| `make verify` | Runs all `hack/verify-*.sh` scripts (boilerplate, dependencies, unicode, import order, lint). |

Tools needed by the scripts (including kind and kubectl) are downloaded on first use at the pinned
versions in [hack/lib.sh](hack/lib.sh) into `_output/tools/`.

## Usage

```sh
make build

_output/kcp-apiexport-proxy \
  --kubeconfig=/path/to/kubeconfig \
  --apiexportendpointslice-names=my-export,my-other-export \
  --bind-address=:8443 \
  --tls-cert-file=/path/to/tls.crt \
  --tls-key-file=/path/to/tls.key \
  --token-file=/path/to/token
```

| Flag | Description |
| --- | --- |
| `--kubeconfig` | Kubeconfig whose current context points at the workspace containing the `APIExportEndpointSlice`; also used as the identity for all requests to shards. Required. |
| `--apiexportendpointslice-names` | Comma-separated names of the `APIExportEndpointSlices` to watch, all in the kubeconfig's workspace. Required. |
| `--bind-address` | Listen address (default `:8080`). |
| `--token-file` | File containing the bearer token clients must send for the proxied paths; reloaded when it changes. Requires `--tls-cert-file` and `--tls-key-file`. Optional, no authentication if not set. |
| `--tls-cert-file` | PEM encoded serving certificate; reloaded when it changes. Optional, plain HTTP if not set. |
| `--tls-key-file` | PEM encoded private key for `--tls-cert-file`; reloaded when it changes. Required with `--tls-cert-file`. |
| `--version` | Prints the version and exits. |

Besides the proxied `/apiexportendpointslices/...` paths, the server exposes `/healthz`,
`/readyz` and `/metrics`, which never require the token.

Example request:

```sh
curl --cacert /path/to/ca.crt \
  --header "Authorization: Bearer $(cat /path/to/token)" \
  https://localhost:8443/apiexportendpointslices/my-export/clusters/<logical-cluster>/apis/apis.kcp.io/v1alpha1/apibindings
```

A sample Kyverno `ValidatingPolicy` that uses the proxy can be found in
[config/samples/policy.yaml](config/samples/policy.yaml).

## License

Apache License 2.0, see [LICENSE](LICENSE).
