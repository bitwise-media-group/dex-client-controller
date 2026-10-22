# Status and deletion

Both kinds are namespaced, in API group `dex.bitwisemedia.uk/v1alpha1`, category `dex` (`kubectl get dex -A`). Each has
a status subresource with `observedGeneration` and a single `Ready` condition, and prints its dex ID, readiness and age.

- [DexClient](dexclient.md) — an OAuth2 client.
- [DexConnector](dexconnector.md) — an upstream identity provider.

## The Ready condition

| Reason            | Meaning                                                                         |
| ----------------- | ------------------------------------------------------------------------------- |
| `Synced`          | dex holds exactly what the spec describes                                       |
| `SecretNotFound`  | a referenced Secret or key is missing or empty; retried when the Secret changes |
| `InvalidConfig`   | the spec cannot be turned into a dex object                                     |
| `Conflict`        | another resource of the same kind claims the ID; the oldest wins                |
| `ReadOnly`        | the ID belongs to a static client/connector in dex's config file                |
| `FeatureDisabled` | dex runs without `DEX_API_CONNECTORS_CRUD=true`                                 |
| `DexUnavailable`  | dex's gRPC API is unreachable; retried with backoff                             |
| `DexRejected`     | dex answered with an error; the message carries dex's                           |

## Deletion

A finalizer (`dex.bitwisemedia.uk/finalizer`) holds the resource until dex confirms the delete. Two exceptions: a
`ReadOnly` resource is released without touching dex, and when another resource still claims the same ID, it takes the
dex entry over instead of it being deleted. A connector stays in dex — and the finalizer stays on the resource — while
dex refuses connector changes; remove the finalizer by hand if dex will never get the flag back.
