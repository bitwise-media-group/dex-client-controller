# Kubescape exceptions

`exceptions.json` is auto-loaded by the toolchain's kubescape gate (`.mise/common/scripts/lint-helm.sh`). JSON carries
no comments, so the rationale for each accepted finding lives here. Every entry is an accepted design decision or a
scanner false positive — none is a deferred fix. Resource `name` attributes are regexes; the leading `.*` absorbs the
release-name prefix.

## controller-c0211-selinux-options-are-host-specific (C-0211)

The Deployment already sets the full security context: `runAsNonRoot`, non-zero uid/gid, `fsGroup`, seccomp
`RuntimeDefault`, `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem`, all capabilities dropped. The only field
C-0211 still wants is `seLinuxOptions`, and an MCS level is a property of the node OS / cluster policy, not something a
portable chart can invent.

## controller-needs-the-kubernetes-api (C-0034, C-0053, C-0190, C-0261)

The controller's whole job is watching and writing custom resources, so its ServiceAccount token mount is load-bearing.
The scanner flags any mounted token; it cannot know this one is required.

## controller-rbac-reads-referenced-secrets (C-0015, C-0186, C-0267)

DexClients and DexConnectors reference Secrets by name in their own namespace, and the controller generates (and owns)
client Secrets, so it needs `get`/`create`/`update` on Secrets plus `list`/`watch` for its metadata-only Secret watch
(which maps a changed Secret to the objects that read it). Secret data never enters the controller's cache: reads go
through the uncached API reader. Scope it with `watchNamespaces`, which binds the ClusterRole into only those namespaces
with RoleBindings.

## image-signed-but-scanner-cannot-see-it (C-0236, C-0237)

The image IS signed: GoReleaser's `docker_signs` runs keyless cosign in the release workflow (GitHub Actions OIDC
identity), which as of cosign 3.x attaches signatures in the sigstore bundle format via the OCI referrers API — not the
legacy `sha256-<digest>.sig` tag scheme kubescape's check looks for (and before the first release there is no image to
check at all). Verify for yourself:

    cosign verify \
      --certificate-identity-regexp '^https://github.com/bitwise-media-group/' \
      --certificate-oidc-issuer https://token.actions.githubusercontent.com \
      ghcr.io/bitwise-media-group/dex-client-controller:vX.Y.Z

Drop this entry if/when kubescape's signature rule learns the bundle format.
