// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package dex

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v1alpha1 "github.com/bitwise-media-group/dex-client-controller/api/v1alpha1"
)

// The error messages dex's API server produces (server/apiserver and
// storage/static.go upstream). They cross the wire as gRPC code Unknown, so
// the message is the only signal.
const (
	// msgNotFound is storage.ErrNotFound, which GetClient returns as-is.
	msgNotFound = "not found"
	// msgFeatureDisabled is the connector RPCs' answer when dex runs
	// without DEX_API_CONNECTORS_CRUD=true.
	msgFeatureDisabled = "api_connectors_crud feature flag is not enabled"
	// msgReadOnly is the static clients/connectors storage refusing a write.
	msgReadOnly = "read-only cannot"
)

// FeatureFlagEnv is the environment variable that enables dex's connector
// CRUD API.
const FeatureFlagEnv = "DEX_API_CONNECTORS_CRUD"

// IsNotFound reports whether err is dex's "not found".
func IsNotFound(err error) bool {
	s, ok := status.FromError(err)
	return ok && (s.Code() == codes.NotFound || s.Message() == msgNotFound)
}

// IsFeatureDisabled reports whether dex refused a connector RPC because its
// connector CRUD feature flag is off.
func IsFeatureDisabled(err error) bool {
	return err != nil && strings.Contains(err.Error(), msgFeatureDisabled)
}

// IsReadOnly reports whether dex refused a write to a static client or
// connector (one defined in dex's config file).
func IsReadOnly(err error) bool {
	return err != nil && strings.Contains(err.Error(), msgReadOnly)
}

// IsUnavailable reports whether err means dex could not be reached (as
// opposed to dex answering with an error).
func IsUnavailable(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	s, ok := status.FromError(err)
	if !ok {
		return false
	}
	switch s.Code() {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return true
	}
	return false
}

// Reason maps an error from a dex RPC to a Ready condition reason and a
// message for the user.
func Reason(err error) (reason, message string) {
	msg := err.Error()
	if s, ok := status.FromError(err); ok {
		msg = s.Message()
	}
	switch {
	case IsFeatureDisabled(err):
		return v1alpha1.ReasonFeatureDisabled,
			"dex refuses connector changes: run dex with " + FeatureFlagEnv + "=true"
	case IsReadOnly(err):
		return v1alpha1.ReasonReadOnly,
			"the ID belongs to a static entry in dex's config file, which the API cannot change: " + msg
	case IsUnavailable(err):
		return v1alpha1.ReasonDexUnavailable, "dex gRPC API unreachable: " + msg
	default:
		return v1alpha1.ReasonDexRejected, "dex rejected the request: " + msg
	}
}
