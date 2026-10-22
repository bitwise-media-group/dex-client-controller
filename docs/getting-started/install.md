# Install

## 1. Enable dex's gRPC API with mutual TLS

dex's API has no authentication beyond TLS client certificates: whoever holds a certificate signed by `tlsClientCA`
administers dex. Enable it in dex's config:

```yaml
grpc:
  addr: 0.0.0.0:5557
  tlsCert: /etc/dex/grpc/tls.crt
  tlsKey: /etc/dex/grpc/tls.key
  tlsClientCA: /etc/dex/grpc/ca.crt
```

To manage connectors as well as clients, run dex with the connector CRUD API enabled:

```yaml
env:
  - name: DEX_API_CONNECTORS_CRUD
    value: "true"
```

Without it, `DexClient` works and every `DexConnector` reports `Ready=False, reason FeatureDisabled`.

## 2. Issue the controller's client certificate

The chart mounts a Secret (default `<release>-dex-client-controller-dex-tls`) with `ca.crt` (verifies dex's serving
certificate), `tls.crt` and `tls.key` (signed by dex's `tlsClientCA`). With cert-manager:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: dex-client-controller-dex-tls
  namespace: dex-client-controller
spec:
  secretName: dex-client-controller-dex-tls
  commonName: dex-client-controller
  usages: ["client auth"]
  issuerRef:
    kind: Issuer
    name: dex-grpc-ca # the issuer whose CA is dex's tlsClientCA
```

The controller re-reads the certificate on every reconnect, so rotation needs no restart.

## 3. Install the chart

```console
helm install dex-client-controller \
  oci://ghcr.io/bitwise-media-group/dex-client-controller/charts/dex-client-controller \
  --namespace dex-client-controller --create-namespace \
  --set dex.address=dex.dex.svc.cluster.local:5557 \
  --set dex.issuer=https://dex.example.com \
  --set dex.tls.secretName=dex-client-controller-dex-tls
```

Useful values:

| Value                            | Default                          | Meaning                                                    |
| -------------------------------- | -------------------------------- | ---------------------------------------------------------- |
| `dex.address`                    | `dex.dex.svc.cluster.local:5557` | dex's gRPC address                                         |
| `dex.issuer`                     | `""`                             | written into generated client Secrets as `issuer`          |
| `dex.tls.secretName`             | `<fullname>-dex-tls`             | the mTLS Secret                                            |
| `watchNamespaces`                | `[]` (all)                       | namespaces to reconcile; also scopes the controller's RBAC |
| `resyncPeriod`                   | `5m`                             | drift-repair interval                                      |
| `rbac.aggregateToDefaultRoles`   | `true`                           | let namespace `edit`/`admin` create DexClients             |
| `networkPolicy.enabled`          | `false`                          | default-deny NetworkPolicy for the controller pod          |
| `metrics.serviceMonitor.enabled` | `false`                          | Prometheus Operator ServiceMonitor                         |

The chart installs the CRDs as templates (so upgrades update them) and keeps them on uninstall (`crds.keep`).

## 4. Decide who may create connectors

A connector changes how everyone signs in. The chart aggregates `DexClient` write access into the built-in
`edit`/`admin` roles but `DexConnector` only into `view`. Grant connector authors explicitly:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: dex-connector-admin
rules:
  - apiGroups: ["dex.bitwisemedia.uk"]
    resources: ["dexconnectors"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

## Verify supply-chain signatures

```console
cosign verify ghcr.io/bitwise-media-group/dex-client-controller:vX.Y.Z \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com \
  --certificate-identity-regexp='^https://github.com/bitwise-media-group/github-workflows/'
gh attestation verify --owner bitwise-media-group \
  oci://ghcr.io/bitwise-media-group/dex-client-controller/charts/dex-client-controller:X.Y.Z
```
