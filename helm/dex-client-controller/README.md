# dex-client-controller Helm chart

Installs [dex-client-controller](https://github.com/bitwise-media-group/dex-client-controller): the `DexClient` and
`DexConnector` CRDs, the controller Deployment, its RBAC, and optionally a metrics ServiceMonitor and a NetworkPolicy.

```console
helm install dex-client-controller \
  oci://ghcr.io/bitwise-media-group/dex-client-controller/charts/dex-client-controller \
  --namespace dex-client-controller --create-namespace \
  --set dex.address=dex.dex.svc.cluster.local:5557 \
  --set dex.tls.secretName=dex-client-controller-dex-tls
```

Prerequisites, all described in the post-install notes:

- dex's gRPC API enabled with mutual TLS (`grpc.tlsClientCA`);
- a Secret with `ca.crt`, `tls.crt` and `tls.key` for the controller (e.g. from a cert-manager Certificate);
- `DEX_API_CONNECTORS_CRUD=true` on dex if you use `DexConnector`.

Every value is documented in [values.yaml](values.yaml) and validated by [values.schema.json](values.schema.json). See
the [install guide](https://oss.bitwisemedia.uk/dex-client-controller/getting-started/install/) for the walkthrough.
