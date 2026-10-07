# kcp-apiexport-proxy

`apiexport-proxy` is a small reverse proxy that sits in front of the
shard-specific virtual workspace endpoints of a single kcp `APIExport`. It
gives clients (for example Kyverno policies using the
[HTTP CEL library](https://kyverno.io/docs/policy-types/cel-libraries/)) one
stable URL through which they can reach any logical cluster bound to that
`APIExport`, regardless of which shard it lives on.

## How it works

1. Watches a named `APIExportEndpointSlice` for the set of per-shard virtual
   workspace URLs.
2. Watches `APIBindings` through each of those URLs and builds an index from
   logical cluster name to virtual workspace URL.
3. Forwards requests of the form `/clusters/<logical-cluster>/...` to the
   right shard, using the identity from the configured kubeconfig. Requests
   for unknown logical clusters get a `404`.

The proxy serves plain HTTP and performs **no authentication**; it is meant
to be reachable only from trusted in-cluster clients.

## Usage

```sh
go build -o apiexport-proxy ./cmd/apiexport-proxy

./apiexport-proxy \
  --kubeconfig=/path/to/kubeconfig \
  --apiexportendpointslice-name=my-export \
  --bind-address=:8080
```

| Flag | Description |
| --- | --- |
| `--kubeconfig` | Kubeconfig whose current context points at the workspace containing the `APIExportEndpointSlice`; also used as the identity for all requests to shards. Required. |
| `--apiexportendpointslice-name` | Name of the `APIExportEndpointSlice` to watch. Required. |
| `--bind-address` | Listen address (default `:8080`). |

Besides the proxied `/clusters/...` paths, the server exposes `/healthz`,
`/readyz` and `/metrics`.

Example request:

```sh
curl http://localhost:8080/clusters/<logical-cluster>/apis/apis.kcp.io/v1alpha1/apibindings
```

A sample Kyverno `ValidatingPolicy` that uses the proxy can be found in
[config/samples/policy.yaml](config/samples/policy.yaml).

## License

Apache License 2.0, see [LICENSE](LICENSE).
