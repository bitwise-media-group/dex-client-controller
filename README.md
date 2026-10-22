# dex-client-controller

Self-service SSO for [dex](https://dexidp.io): namespaced `DexClient` and `DexConnector` custom resources, reconciled
into a running dex through its gRPC API.

Application teams usually self-serve SSO by having something rewrite dex's central `config.yaml`. That restarts dex on
every change, puts client secrets in a ConfigMap, leaves one central file no application owns, and couples the tooling
to dex's config format ([dexidp/dex#4766](https://github.com/dexidp/dex/discussions/4766)). This controller does it from
outside dex instead, through the API dex already ships:

- **No restarts.** Clients and connectors are created and updated live; dex reloads a changed connector lazily.
- **Secrets stay Secrets.** Resources reference Kubernetes Secrets, or the controller generates one it owns.
- **GitOps-friendly.** Each team's resources live in its own namespace; IDs default to `<namespace>-<name>`.
- **Any storage backend**, and additive: `staticClients`/`staticConnectors` keep working and are never touched.

```yaml
apiVersion: dex.bitwisemedia.uk/v1alpha1
kind: DexClient
metadata:
  name: grafana
  namespace: monitoring
spec:
  name: Grafana
  redirectURIs: [https://grafana.example.com/login/generic_oauth]
```

registers client `monitoring-grafana` in dex and writes `clientID`, `clientSecret` and `issuer` into the Secret
`monitoring/grafana-dex-client`.

## Install

dex needs its gRPC API enabled with mutual TLS (`grpc.tlsClientCA`), and `DEX_API_CONNECTORS_CRUD=true` if you use
`DexConnector`. Then:

```console
helm install dex-client-controller \
  oci://ghcr.io/bitwise-media-group/dex-client-controller/charts/dex-client-controller \
  --namespace dex-client-controller --create-namespace \
  --set dex.address=dex.dex.svc.cluster.local:5557 \
  --set dex.tls.secretName=dex-client-controller-dex-tls
```

See [docs/getting-started/install.md](docs/getting-started/install.md) for the certificate setup and RBAC, and
[docs/resources/](docs/resources/) for both resources.

## Development

Everything runs through [mise](https://mise.jdx.dev) tasks (`make <task>` forwards to `mise run <task>`); the pinned
tools come from the shared toolchain submodule at `.mise/`.

```console
git submodule update --init
mise run ci        # lint (incl. codegen drift), test (incl. envtest), build
mise run codegen   # regenerate deepcopy + CRDs after editing api/v1alpha1
mise run e2e       # real dex + real controller binary against envtest
mise run snapshot  # local release build: binaries, archives, SBOMs, image
```

Design notes live in [DESIGN.md](DESIGN.md); the repository map for contributors and agents in [AGENTS.md](AGENTS.md).

## License

[MIT](LICENSE)
