---
description:
  "An upstream identity provider in dex — OIDC, LDAP, SAML, GitHub and the rest — with secret values merged into its
  config."
---

# DexConnector

A `DexConnector` registers one upstream identity provider in dex. It needs dex to run with
`DEX_API_CONNECTORS_CRUD=true`; without it, every DexConnector reports `Ready=False, reason FeatureDisabled`.

```yaml
apiVersion: dex.bitwisemedia.uk/v1alpha1
kind: DexConnector
metadata:
  name: azure-ad
  namespace: platform
spec:
  id: "" # default "<namespace>-<name>"; immutable
  type: oidc # any connector type dex was built with
  name: Azure AD # default: the resource's name
  config: # the type's config, exactly as in dex's config file
    issuer: https://login.microsoftonline.com/<tenant>/v2.0
    clientID: 00000000-0000-0000-0000-000000000000
    redirectURI: https://dex.example.com/callback
    getUserInfo: true
  secretRefs: # secret values merged into config
    - configField: clientSecret # dotted path: "bindPW", "nested.key"
      valueFrom:
        name: azure-ad-creds
        key: clientSecret
  grantTypes: [] # empty leaves the connector unrestricted
status:
  id: platform-azure-ad
  observedGeneration: 1
  conditions:
    - type: Ready
      status: "True"
      reason: Synced
```

## Secret values and updates

Each `secretRefs` entry writes the Secret value at its dotted `configField` path, creating intermediate objects as
needed; a path through a value that is not an object is `InvalidConfig`. The merged config is compared with dex's in
canonical form, and the connector is updated only on a real difference — each update bumps the connector's resource
version, which makes dex rebuild it on next use.
