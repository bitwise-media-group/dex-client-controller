# Overview

Self-service SSO for [dex](https://dexidp.io). Application teams declare the OAuth2 clients and upstream identity
providers they need as namespaced Kubernetes resources — `DexClient` and `DexConnector` — and the controller registers
them in a running dex through its gRPC API.

Compared with rewriting dex's central `config.yaml`:

| Rewriting the config file                       | dex-client-controller                                                      |
| ----------------------------------------------- | -------------------------------------------------------------------------- |
| dex restarts on every change                    | changes go through the API; dex reloads a connector lazily, never restarts |
| client and connector secrets sit in a ConfigMap | secrets stay in Kubernetes Secrets the resources reference                 |
| one central file no application owns            | each team owns its resources, in its own namespace, in its own GitOps repo |
| the controller must track dex's config format   | the controller speaks dex's versioned gRPC API                             |

It works with any dex storage backend, and is additive: `staticClients` and `staticConnectors` in dex's config keep
working, and the controller refuses to touch them.

## A client in one resource

```yaml
apiVersion: dex.bitwisemedia.uk/v1alpha1
kind: DexClient
metadata:
  name: grafana
  namespace: monitoring
spec:
  name: Grafana
  redirectURIs:
    - https://grafana.example.com/login/generic_oauth
```

The controller registers client ID `monitoring-grafana`, generates a secret, and writes `clientID`, `clientSecret` and
`issuer` into the Secret `monitoring/grafana-dex-client`, ready for Grafana to mount.

```console
$ kubectl get dexclients -n monitoring
NAME      ID                   READY   AGE
grafana   monitoring-grafana   True    12s
```

## Next

- [Install](install.md) the controller and open dex's API to it.
- Read [how it works](../concepts/how-it-works.md): IDs, conflicts, Secrets and drift.
- Look up every field of [`DexClient`](../resources/dexclient.md) and [`DexConnector`](../resources/dexconnector.md).
