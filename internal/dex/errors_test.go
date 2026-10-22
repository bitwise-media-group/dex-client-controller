// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package dex

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v1alpha1 "github.com/bitwise-media-group/dex-client-controller/api/v1alpha1"
)

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{status.Error(codes.Unknown, "not found"), true},
		{status.Error(codes.NotFound, "client gone"), true},
		{status.Error(codes.Unknown, "create client: not found in cache"), false},
		{errors.New("not found"), false},
		{nil, false},
	}
	for _, tt := range tests {
		if got := IsNotFound(tt.err); got != tt.want {
			t.Errorf("IsNotFound(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func TestReason(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			"feature flag",
			status.Error(codes.Unknown, "api_connectors_crud feature flag is not enabled"),
			v1alpha1.ReasonFeatureDisabled,
		},
		{
			"static client",
			status.Error(codes.Unknown, "update client: static clients: read-only cannot update client"),
			v1alpha1.ReasonReadOnly,
		},
		{"unavailable", status.Error(codes.Unavailable, "connection refused"), v1alpha1.ReasonDexUnavailable},
		{"deadline", fmt.Errorf("get: %w", context.DeadlineExceeded), v1alpha1.ReasonDexUnavailable},
		{"rejected", status.Error(codes.Unknown, "invalid config supplied"), v1alpha1.ReasonDexRejected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, msg := Reason(tt.err)
			if got != tt.want {
				t.Errorf("Reason = %q (%s), want %q", got, msg, tt.want)
			}
			if msg == "" {
				t.Error("Reason returned an empty message")
			}
		})
	}
}
