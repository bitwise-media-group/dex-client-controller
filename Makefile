# Copyright 2026 BitWise Media Group Ltd
# SPDX-License-Identifier: MIT
#
# dex-client-controller — reconciles namespaced DexClient/DexConnector custom
# resources into a running dex through its gRPC API.
#
# Everything lives in mise tasks: the go archetype (test/lint/release
# machinery) and the common tasks (license, prose, workflow, container, and
# shell lint) come from the shared toolchain submodule at .mise/, selected in
# the root mise.toml; tasks.toml carries the repo-local tasks (codegen, e2e,
# and the go-lint/go-test overrides). This Makefile is only the thin
# forwarding shim — `make <task>` == `mise run <task>`.
MISE_TASKS += codegen
include .mise/archetypes/go/include.mk
