#!/usr/bin/env sh
# Copyright 2026 BitWise Media Group Ltd
# SPDX-License-Identifier: MIT
#
# The unit tests, with the envtest suites (CRD schema/CEL validation against
# a real kube-apiserver) running for real rather than skipping: provision the
# kube-apiserver/etcd binaries, export KUBEBUILDER_ASSETS, then hand off to
# the toolchain's go test script unchanged (gotestsum, -race, coverage/,
# junit). setup-envtest downloads the binaries once into ~/.cache (the
# default store under ~/Library/Application Support is not writable in
# sandboxed agent runs); ENVTEST_K8S_VERSION pins a release (default: the
# newest the tool lists).
set -eu

KUBEBUILDER_ASSETS=$(setup-envtest use ${ENVTEST_K8S_VERSION:+"$ENVTEST_K8S_VERSION"} \
    --bin-dir "${HOME}/.cache/kubebuilder-envtest" -p path)
export KUBEBUILDER_ASSETS
exec .mise/archetypes/go/scripts/test.sh
