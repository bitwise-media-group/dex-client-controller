// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"

	"github.com/bitwise-media-group/dex-client-controller/internal/cli"
)

// newServeCmd is the serve subcommand; the reconcilers are wired in once they
// exist. Until then it idles until signalled.
func newServeCmd(_ *cli.Options) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the DexClient and DexConnector reconcilers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			<-cmd.Context().Done()
			return nil
		},
	}
}
