# Security Policy

## Reporting a vulnerability

Please report vulnerabilities privately via
[GitHub Security Advisories](https://github.com/bitwise-media-group/dex-client-controller/security/advisories/new). Do
not open public issues for security reports.

## Threat model (summary)

dex-client-controller holds administrative access to one dex: dex's gRPC API has no authorization model, so the
controller's client certificate can create, change and delete any client or connector. Its security surface is that
certificate, the Kubernetes Secrets it reads, and who may create the resources it acts on. It defends against:

- **Unauthenticated access to dex's API** — the controller only dials dex with mutual TLS (`--dex-insecure` exists for
  local development and logs a warning). The client certificate is mounted read-only and never logged.
- **Secret sprawl** — client and connector secrets live only in Kubernetes Secrets and dex's storage. The controller
  never caches Secret contents (metadata-only watch, uncached reads) and never writes a secret into a status, condition,
  event or log line.
- **Cross-namespace interference** — dex IDs default to `<namespace>-<name>`, references resolve only within the
  object's own namespace, and a claim on an ID already held by an older object is refused (`Conflict`). A generated
  Secret name already taken by a Secret the DexClient does not own is refused rather than adopted.
- **Clobbering operator configuration** — dex's static clients and connectors are read-only through the API; the
  controller reports `ReadOnly` and never deletes them.
- **Over-broad connector authorship** — a connector changes how everyone signs in, so the chart aggregates
  `DexConnector` write access into no built-in role; it must be granted explicitly.
- **Tampered release artifacts** — releases ship `checksums.txt`, a SLSA build-provenance attestation, keyless Sigstore
  (cosign) signatures for the binaries, image and chart, and an SPDX SBOM per binary.

Out of scope: the security of dex itself and of the identity providers it federates to; a compromise of the cluster the
controller runs in, or of anyone holding a certificate signed by dex's `tlsClientCA`; and a compromise of the release
workflow's signing identity.
