# dex-client-controller design

## Problem

[dexidp/dex#4766](https://github.com/dexidp/dex/discussions/4766) describes a common pattern: application teams
self-serve SSO by having a controller rewrite dex's central `config.yaml` ConfigMap. That has four costs:

1. dex restarts on every change;
2. client and connector secrets sit in plaintext in a ConfigMap;
3. one central ConfigMap that no single application owns breaks GitOps;
4. the controller must track dex's config format.

The discussion proposes namespaced `DexConnector`/`DexClient` CRDs reconciled live, with secrets referenced from
Kubernetes Secrets, working on any storage backend, and additive to `staticClients`/`staticConnectors`. This project
does exactly that **outside dex**, through dex's gRPC API (`github.com/dexidp/dex/api/v2`).

## Decisions

- **Backend: the dex gRPC API.** It works with any storage backend. Clients are always available; connectors need dex to
  run with `DEX_API_CONNECTORS_CRUD=true`. dex reloads a connector lazily when its `ResourceVersion` changes, so it
  never restarts.
- **Targeting: one dex per controller**, configured by flags / Helm values (gRPC address plus an mTLS Secret).
- **Client secret:** `spec.secretRef` points at an existing Secret. Without one, the controller generates a secret and
  writes it to a Secret it owns.
- **Scope:** both CRDs are namespaced; who may create them is RBAC's job. dex IDs default to `<namespace>-<name>` so
  namespaces cannot collide.

## Facts about dex that shape the design

Checked against `dexidp/dex@master` (`server/apiserver/*.go`, `storage/static.go`, `api/v2/api.proto`).

- **`UpdateClient` cannot change `secret` or `public`.** Changing either means `DeleteClient` then `CreateClient`.
- **`UpdateClient` ignores an empty `name` or `logo_url`, and cannot empty a list** — an empty repeated field is
  indistinguishable from an absent one on the wire. Clearing any of those also means delete-then-create. The two
  optional strings, `backchannel_logout_uri` and `refresh_token_lifetime`, have explicit presence and are always sent,
  so they can be cleared in place.
- **`GetClient` returns the secret**, so secret drift is detected by direct comparison; no hash is stored.
- **There is no `GetConnector`.** The controller lists connectors and finds its own by ID.
- **Connector `config` must be valid JSON bytes**, and grant types must be among those dex lets a connector be
  restricted to.
- **The connector RPCs fail with `api_connectors_crud feature flag is not enabled`** while the flag is off.
- **Errors are untyped.** dex returns plain Go errors, which reach the client as gRPC code `Unknown`; the message
  (`not found`, `... read-only cannot ...`, the feature-flag text) is the only signal. `internal/dex/errors.go`
  classifies on those messages.
- **The API has no authentication except TLS client certificates**, so the controller always dials with mTLS (an
  `--dex-insecure` escape hatch exists for local development).
- **dex has no events and no labels.** Drift — such as memory storage wiped by a restart — is repaired by a periodic
  resync, and there is nowhere to record ownership in dex itself.
- **The dex API module's last tag (`api/v2.4.0`) predates `Client.refresh_token_lifetime` and
  `UpdateConnectorReq.new_grant_types`,** so `go.mod` pins a pseudo-version from master.

## API (`dex.bitwisemedia.uk/v1alpha1`)

Both kinds have a status subresource, category `dex`, print columns (ID, Ready, Age), and a status of
`observedGeneration` plus `conditions`. Field-level documentation is in [docs/resources/](docs/resources/).

- **`DexClient`** — `clientID` (immutable, default `<ns>-<name>`), `name`, `public`, `redirectURIs`,
  `postLogoutRedirectURIs`, `trustedPeers`, `allowedConnectors`, `logoURL`, `backchannelLogoutURI`,
  `refreshTokenLifetime`, and either `secretRef` or `generatedSecret`. CEL rules: `redirectURIs` is required unless
  `public`; `secretRef`/`generatedSecret` are mutually exclusive and forbidden on a public client.
- **`DexConnector`** — `id` (immutable, default `<ns>-<name>`), `type`, `name` (default: the object's name), free-form
  `config` (`x-kubernetes-preserve-unknown-fields`, must be an object), `secretRefs` (Secret values merged in at dotted
  `configField` paths), and `grantTypes`.

The single `Ready` condition carries one of: `Synced`, `SecretNotFound`, `InvalidConfig`, `Conflict`, `ReadOnly`,
`FeatureDisabled`, `DexUnavailable`, `DexRejected`.

## Reconciliation

Both reconcilers share one shape (`internal/controller`):

- A finalizer, `dex.bitwisemedia.uk/finalizer`, holds the object until dex confirms the delete.
- `RequeueAfter: --resync-period` (default 5m) repairs drift. dex being unreachable is returned as an error instead, for
  controller-runtime's rate-limited backoff.
- **Secrets are never cached.** The manager's client bypasses the cache for Secrets and contents are read through the
  API reader. Secrets are watched metadata-only, and a field index of the Secret names each object references maps a
  Secret change back to the objects to reconcile.
- **Conflicts:** a field index on the resolved dex ID finds every object claiming an ID. The oldest by
  `creationTimestamp` (ties: namespace, then name) wins; the others report `Conflict`. Objects being deleted give up
  their claim, so when the winner is deleted while a younger claimant exists, the younger takes the dex entry over
  rather than it being deleted. A second watch on each kind enqueues the other claimants of an ID whenever one changes,
  so the handover is immediate.
- **Static entries:** dex refuses writes to IDs from its config file with a `read-only` error. That becomes `ReadOnly`,
  and deleting such an object releases it without touching dex.

### DexClient

1. On deletion: unless another live DexClient claims the ID, `DeleteClient` (not-found and read-only count as done),
   then remove the finalizer.
2. Resolve the secret: from `secretRef` through the API reader; or from the generated Secret, creating it (32 random
   bytes, base64url, owner reference, keys `clientID`, `clientSecret`, plus `issuer` with `--dex-issuer`) when it is
   missing and refilling its keys when they are. A pre-existing Secret of that name that the DexClient does not control
   is `InvalidConfig`, never adopted. Public clients have no secret.
3. `GetClient`. Not found: `CreateClient`. Secret or `public` differs, or a field must be cleared that `UpdateClient`
   cannot clear: `DeleteClient` then `CreateClient`. Any other difference: `UpdateClient` with the full field set.
4. Status: `clientID`, `secretName`, `Ready`.

### DexConnector

1. On deletion: unless another live DexConnector claims the ID, `DeleteConnector`. A refusal (including
   `FeatureDisabled`) keeps the finalizer: a connector left in dex keeps accepting logins.
2. Read each referenced Secret, unmarshal `spec.config` (numbers kept exact), set each value at its `configField` path,
   marshal canonically (sorted keys, no HTML escaping).
3. `ListConnectors`, find by ID. Absent: create. Otherwise compare type, name, canonical config and grant types, and
   `UpdateConnector` only on a difference, so dex's `ResourceVersion` — and with it the connector instance — stays
   stable.

## Code layout

```text
cmd/dex-client-controller/        cobra main + serve
internal/cli/                     root command, flag > DEX_CLIENT_CONTROLLER_* env > default
internal/kube/                    manager: scheme, slog→logr, health/metrics, leader election, Secret cache off
internal/dex/                     Client interface (9 RPCs), mTLS dial, readyz check, error classification
internal/dex/dextest/             in-memory dex fake with dex's API-server semantics; test certificates
internal/controller/              shared: indexes, claimant, Secret reads, Ready condition, finalizers
internal/controller/dexclient/    DexClient reconciler
internal/controller/dexconnector/ DexConnector reconciler + config merge
api/v1alpha1/                     types (apimachinery-only), CEL schema test (envtest)
config/crd/, helm/                generated CRDs; the chart (CRDs mirrored as templates)
e2e/                              separate module: real dex + real binary against envtest
```

## Verification

- `mise run ci` — lint (golangci-lint, govulncheck, the codegen drift check, license/prose, actionlint/zizmor,
  hadolint/grype, helm lint/kubescape), test (unit + envtest), build.
- `mise run e2e` — the `e2e/` module builds dex from its `tool` directive and the controller binary, runs two dex
  processes (memory storage, mTLS gRPC with certificates generated at test time; connector CRUD on and off) and one
  controller per dex (each confined to a namespace), and asserts: a generated-secret client reaches dex; rotating a
  referenced Secret rotates the client (with a one-hour resync, so only the Secret watch can have done it); a static
  client ID is `ReadOnly`; a connector's Secret value lands in the config `ListConnectors` returns; deletes remove both
  from dex; the flagless dex yields `FeatureDisabled` for connectors while still taking clients.
- `mise run snapshot` — local release build without signing.
