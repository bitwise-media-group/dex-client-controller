// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Command dex-client-controller reconciles namespaced DexClient and
// DexConnector custom resources into a running dex through its gRPC API, so
// application teams self-serve SSO without editing dex's central config and
// without dex restarting.
package main

import (
	"os"

	"github.com/bitwise-media-group/dex-client-controller/internal/cli"
)

func main() {
	opts := cli.NewOptions()
	root := cli.NewControllerRoot("dex-client-controller",
		"Reconcile DexClient and DexConnector resources into dex through its gRPC API", opts)
	root.AddCommand(newServeCmd(opts))
	os.Exit(cli.Execute(root, opts.Log))
}
