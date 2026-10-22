---
description:
  "An OAuth2 client in dex: redirect URIs, trusted peers and allowed connectors, with its secret referenced from a
  Secret or generated into one."
---

# DexClient

A `DexClient` registers one OAuth2 client in dex. It works on any dex: the client API needs no feature flag.

```yaml
apiVersion: dex.bitwisemedia.uk/v1alpha1
kind: DexClient
metadata:
  name: grafana
  namespace: monitoring
spec:
  clientID: "" # default "<namespace>-<name>"; immutable
  name: Grafana
  public: false
  redirectURIs: [https://grafana.example.com/login/generic_oauth]
  postLogoutRedirectURIs: []
  trustedPeers: []
  allowedConnectors: [] # dex connector IDs; empty allows all
  logoURL: ""
  backchannelLogoutURI: ""
  refreshTokenLifetime: "" # "", standalone or session
  secretRef: # use an existing Secret...
    name: grafana-oidc
    key: clientSecret # default clientSecret
  # generatedSecret:           # ...or let the controller generate one
  #   name: ""                 # default "<name>-dex-client"
status:
  clientID: monitoring-grafana
  secretName: grafana-oidc
  observedGeneration: 1
  conditions:
    - type: Ready
      status: "True"
      reason: Synced
```

## Validation

The API server enforces these rules (CEL), so an invalid resource is rejected on apply:

- `redirectURIs` is required unless `public`.
- `secretRef` and `generatedSecret` are mutually exclusive, and neither may be set on a public client.
- `clientID` cannot change after creation — create a new resource instead.

## The client secret

- **`secretRef`** — the controller reads the key from that Secret in the same namespace. Rotating the value rotates the
  client in dex.
- **Neither set** — the controller generates 32 random bytes (base64url) into the Secret named by `generatedSecret.name`
  (default `<name>-dex-client`), owned by the DexClient and deleted with it. Keys: `clientID`, `clientSecret`, and
  `issuer` when the controller runs with `--dex-issuer`. If a Secret of that name already exists and the DexClient does
  not own it, the DexClient reports `InvalidConfig` rather than take it over.
- **`public: true`** — no secret.

## How changes reach dex

dex's `UpdateClient` cannot change a client's secret or `public` flag, cannot clear `name` or `logoURL`, and cannot
empty a list. Changes of that kind replace the client (delete, then create with the same ID); everything else is updated
in place.
