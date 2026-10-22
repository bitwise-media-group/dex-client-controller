# Agent orientation

Fast map of this repository so a new session can act without re-exploring. For _what_ the system must do and why, read
[DESIGN.md](DESIGN.md); for end-user usage, [README.md](README.md) and [docs/](docs/). This file is the "where things
are".

## What this is

`dex-client-controller` (module `github.com/bitwise-media-group/dex-client-controller`) is one controller-runtime
binary that reconciles namespaced `DexClient` and `DexConnector` custom resources (`dex.bitwisemedia.uk/v1alpha1`)
into one running dex through dex's gRPC API, dialed with mTLS. It never edits dex's config file and never restarts dex.

## Layout

- `cmd/dex-client-controller/` — `main.go` (cobra root) and `serve.go` (flags, dial, manager, reconcilers).
- `api/v1alpha1/` — the types. apimachinery-only (plus `apiextensionsv1.JSON`); no controller-runtime imports.
  `zz_generated.deepcopy.go` is generated. `schema_envtest_test.go` checks the CEL rules against a real apiserver.
- `internal/cli/` — root command and flag/env resolution (`DEX_CLIENT_CONTROLLER_*`).
- `internal/kube/` — manager construction. Secrets are excluded from the client cache.
- `internal/dex/` — the `Client` interface (the 9 RPCs used), `Dial`, `ReadyCheck`, and `errors.go`, which classifies
  dex's untyped error messages into Ready reasons.
- `internal/dex/dextest/` — in-memory fake implementing `dex.Client` with dex's API-server quirks, plus `WriteCerts`
  for test mTLS material (used by unit tests and e2e).
- `internal/controller/` — shared reconciler plumbing: field indexes, `Claimant` (oldest wins), `SecretValue`,
  `SetReady`/`PatchStatus`, finalizers, `ConditionError`.
- `internal/controller/dexclient/`, `internal/controller/dexconnector/` — the reconcilers; `dexconnector/config.go`
  is the dotted-path secret merge and canonical JSON.
- `config/crd/` — generated CRDs (envtest reads these). `helm/dex-client-controller/` — the chart; its
  `templates/crds/` are generated copies.
- `e2e/` — separate Go module (`replace ../`); `tool` directives pin dex and the controller. Real dex + real binary
  against envtest.
- `hack/` — `codegen.sh`, `test.sh` (envtest assets → toolchain test script), `e2e.sh`, `govulncheck.sh` (reviewed
  acceptance list).
- `docs/`, `overrides/`, `zensical.toml` — the docs site (Zensical), themed like flux-containers/flux-manifests. The
  landing (`overrides/home.html`) builds its resource cards from the nav's "Resources" section and each page's
  `description` front matter; `docs/reference/docs.md` describes the layout.
- `.mise/` — the shared toolchain submodule (bitwise-media-group/toolchain). Never edit it here; never run
  `mise lock` or `mise upgrade` in this repo.

## Working rules

- Everything runs through mise tasks: `mise run ci` (lint, test, build), `mise run e2e`, `mise run codegen`,
  `mise run snapshot`. `make <task>` forwards to mise.
- After editing `api/v1alpha1`, run `mise run codegen`; lint fails on drift in `api/`, `config/`, `helm/`.
- Never put `''` in a CEL rule inside a Go doc comment: gofmt rewrites it to a typographic quote. Use `has()` instead.
- Tests use the standard library only (no testify, no ginkgo). Reconciler tests are table-style with the
  controller-runtime fake client and `dextest`.
- The fake must stay faithful to dex: when dex behaviour matters to a reconciler, encode it in `dextest` and pin it in
  `dextest/server_test.go`.
- License header on every file: `Copyright 2026 BitWise Media Group Ltd` + `SPDX-License-Identifier: MIT`
  (`mise run license` stamps them).
- Commits are Conventional Commits; signing needs the user's YubiKey, so agents hand commits off via `commit.sh`.
