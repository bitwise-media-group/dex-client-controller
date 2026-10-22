#!/usr/bin/env sh
# Copyright 2026 BitWise Media Group Ltd
# SPDX-License-Identifier: MIT
#
# Run the end-to-end suite (the e2e/ module): an envtest kube-apiserver
# carries the CRDs, a real dex (built from the module's `tool` directive) runs
# on memory storage with an mTLS gRPC listener, and the real controller
# binary reconciles between them. setup-envtest downloads the
# kube-apiserver/etcd binaries once into ~/.cache; the suite skips itself
# when KUBEBUILDER_ASSETS is empty, so a plain `go test` there stays fast.
set -eu

KUBEBUILDER_ASSETS=$(setup-envtest use ${ENVTEST_K8S_VERSION:+"$ENVTEST_K8S_VERSION"} \
    --bin-dir "${HOME}/.cache/kubebuilder-envtest" -p path)
export KUBEBUILDER_ASSETS
cd e2e
go test ./... -count=1 "$@"
