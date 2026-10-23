# Copyright 2026 BitWise Media Group Ltd
# SPDX-License-Identifier: MIT

# Controller image, built by GoReleaser (`dockers_v2` in .goreleaser.yaml),
# which compiles the binary first and hands this file a build context
# containing ONLY that binary, laid out as $TARGETPLATFORM/<binary> (e.g.
# linux/amd64/dex-client-controller). There is no Go toolchain or source
# here: version metadata is already stamped into internal/version by
# GoReleaser's ldflags, and the same buildx invocation produces the
# linux/amd64 + linux/arm64 manifest. Build locally (no publish) with
# `mise run snapshot` — needs docker + buildx.
#
# Pure Go, CGO off, nothing exec'd: distroless static is the whole runtime.
# Runs as uid 65532 ("nonroot") and works under readOnlyRootFilesystem. The
# Deployment supplies the subcommand: args: ["serve", ...].

FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99

ARG TARGETPLATFORM

COPY ${TARGETPLATFORM}/dex-client-controller /usr/local/bin/dex-client-controller

USER 65532:65532
EXPOSE 8080 8081
ENTRYPOINT ["/usr/local/bin/dex-client-controller"]
