// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package dex

import (
	"context"
	"fmt"
	"net/http"
	"time"

	api "github.com/dexidp/dex/api/v2"
)

// ReadyCheck returns a readyz checker (healthz.Checker's signature) that
// calls GetVersion: the controller is ready only while dex answers.
func ReadyCheck(c Client, timeout time.Duration) func(*http.Request) error {
	return func(req *http.Request) error {
		ctx, cancel := context.WithTimeout(req.Context(), timeout)
		defer cancel()
		if _, err := c.GetVersion(ctx, &api.VersionReq{}); err != nil {
			return fmt.Errorf("dex GetVersion: %w", err)
		}
		return nil
	}
}
