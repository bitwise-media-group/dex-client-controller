// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package dex is the controller's view of dex's gRPC API: the subset of RPCs
// the reconcilers call (Client), the mTLS dial, a readiness check, and the
// classification of the plain-text errors dex answers with into the
// conditions the reconcilers report.
//
// dex's API server returns most failures as untyped errors (gRPC code
// Unknown) whose message is the only signal, so classification matches on
// the messages dex's own source produces; see errors.go.
package dex
