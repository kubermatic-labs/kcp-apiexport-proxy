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

The proxy serves plain HTTP and performs **no authentication**; it is meant
to be reachable only from trusted in-cluster clients.

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
  --bind-address=:8080
```

| Flag | Description |
| --- | --- |
| `--kubeconfig` | Kubeconfig whose current context points at the workspace containing the `APIExportEndpointSlice`; also used as the identity for all requests to shards. Required. |
| `--apiexportendpointslice-names` | Comma-separated names of the `APIExportEndpointSlices` to watch, all in the kubeconfig's workspace. Required. |
| `--bind-address` | Listen address (default `:8080`). |

Besides the proxied `/apiexportendpointslices/...` paths, the server exposes `/healthz`,
`/readyz` and `/metrics`.

Example request:

```sh
curl http://localhost:8080/apiexportendpointslices/my-export/clusters/<logical-cluster>/apis/apis.kcp.io/v1alpha1/apibindings
```

A sample Kyverno `ValidatingPolicy` that uses the proxy can be found in
[config/samples/policy.yaml](config/samples/policy.yaml).

## License

Apache License 2.0, see [LICENSE](LICENSE).
