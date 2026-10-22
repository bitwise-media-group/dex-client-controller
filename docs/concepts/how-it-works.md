# How it works

The controller is one controller-runtime binary that reconciles `DexClient` and `DexConnector` resources into one
running dex. It never edits dex's config file and never restarts dex: everything goes through the gRPC API dex already
ships.

## One dex, over mutual TLS

Each controller manages exactly one dex, set by `--dex-address` (the chart's `dex.address`). dex's API has no
authentication beyond TLS client certificates — whoever holds a certificate signed by dex's `tlsClientCA` administers
dex — so the controller always dials with mTLS. It re-reads its certificate on every reconnect, so rotation needs no
restart, and its `/readyz` check calls dex's `GetVersion`: a ready controller is one that reaches dex.

## IDs and conflicts

dex IDs default to `<namespace>-<name>`, so two namespaces cannot collide. A resource can set `clientID` or `id`
explicitly, and that field is immutable.

When two resources of the same kind do claim one ID, the oldest by creation time owns it (ties break on namespace, then
name) and the others report `Ready=False, reason Conflict`. A resource being deleted gives up its claim, so deleting the
owner while a younger claimant exists hands the dex entry over to it instead of deleting it.

## Secrets are never cached

Secret contents are read on demand and never held in the controller's cache. Secrets are watched metadata-only, and an
index of the Secrets each resource references maps a change straight back to the resources that use it, so rotating a
referenced Secret rotates the client or connector in dex within seconds.

## Drift is repaired by resync

dex emits no change events and has no labels, so there is nowhere to record ownership in dex itself and nothing to
watch. Every resource is re-reconciled every `--resync-period` (default `5m`): a dex on memory storage that restarts is
repopulated within one period. An unreachable dex is retried with backoff instead.

## Static entries are off-limits

dex refuses writes to the clients and connectors in its config file. A resource claiming one of those IDs reports
`ReadOnly`, and deleting it releases it without touching dex.

## What a reconcile does

**DexClient:** resolve the secret (from `secretRef`, or the generated Secret, creating it if missing), then `GetClient`.
A missing client is created. A change dex's `UpdateClient` cannot express — the secret, `public`, clearing `name` or
`logoURL`, emptying a list — replaces the client (delete, then create with the same ID). Anything else is updated in
place.

**DexConnector:** read each referenced Secret, merge its value into `spec.config` at the dotted `configField` path, and
compare the result with dex's connector in canonical JSON. The connector is created if absent and updated only on a real
difference, because each update bumps its resource version and makes dex rebuild it.

Both kinds carry a finalizer that holds the resource until dex confirms the delete; see
[status and deletion](../resources/index.md).
