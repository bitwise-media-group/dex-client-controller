// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/bitwise-media-group/dex-client-controller/internal/cli"
	"github.com/bitwise-media-group/dex-client-controller/internal/controller"
	"github.com/bitwise-media-group/dex-client-controller/internal/controller/dexclient"
	"github.com/bitwise-media-group/dex-client-controller/internal/controller/dexconnector"
	"github.com/bitwise-media-group/dex-client-controller/internal/dex"
	"github.com/bitwise-media-group/dex-client-controller/internal/kube"
	"github.com/bitwise-media-group/dex-client-controller/internal/version"
)

func newServeCmd(opts *cli.Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the DexClient and DexConnector reconcilers",
		RunE:  func(cmd *cobra.Command, _ []string) error { return serve(cmd.Context(), opts) },
	}
	f := cmd.Flags()
	f.String("kubeconfig", "", "kubeconfig path (default: in-cluster config)")
	f.String("health-addr", ":8081", "healthz/readyz probe listen address")
	f.String("metrics-addr", ":8080", "Prometheus metrics listen address (empty disables)")
	f.Bool("leader-elect", true, "hold a Lease while reconciling, so two replicas never race")
	f.String("leader-election-namespace", "",
		"namespace of the leader-election Lease (default: POD_NAMESPACE, or the in-cluster namespace)")
	f.String("watch-namespaces", "", "comma-separated namespaces to watch (default: all)")
	f.Duration("resync-period", 5*time.Minute,
		"re-reconcile every object this often, repairing drift in dex (which emits no change events)")

	f.String("dex-address", "", "dex gRPC API address, host:port (required)")
	f.String("dex-ca-file", "", "CA bundle verifying dex's gRPC serving certificate (default: system roots)")
	f.String("dex-cert-file", "", "client certificate presented to dex (must chain to dex's grpc.tlsClientCA)")
	f.String("dex-key-file", "", "private key of --dex-cert-file")
	f.String("dex-server-name", "", "name verified in dex's certificate (default: the host of --dex-address)")
	f.Bool("dex-insecure", false, "dial dex without TLS (development only: leaves dex's API unauthenticated)")
	f.String("dex-issuer", "", "dex's issuer URL, written into generated client Secrets (optional)")
	return cmd
}

func serve(ctx context.Context, opts *cli.Options) error {
	log := opts.Log

	dexClient, closeDex, err := dex.Dial(dex.DialOptions{
		Address:    opts.String("dex-address"),
		CAFile:     opts.String("dex-ca-file"),
		CertFile:   opts.String("dex-cert-file"),
		KeyFile:    opts.String("dex-key-file"),
		ServerName: opts.String("dex-server-name"),
		Insecure:   opts.Bool("dex-insecure"),
	})
	if err != nil {
		return err
	}
	defer func() { _ = closeDex() }()
	if opts.Bool("dex-insecure") {
		log.LogAttrs(ctx, slog.LevelWarn, "dialing dex without TLS: its API is unauthenticated")
	}

	leaderID := ""
	if opts.Bool("leader-elect") {
		leaderID = "dex-client-controller-leader"
	}
	leaderNS := opts.String("leader-election-namespace")
	if leaderNS == "" {
		leaderNS = os.Getenv("POD_NAMESPACE")
	}
	namespaces := opts.List("watch-namespaces")
	mgr, err := kube.NewManager(kube.Options{
		Kubeconfig:              opts.String("kubeconfig"),
		LeaderElectionID:        leaderID,
		LeaderElectionNamespace: leaderNS,
		Namespaces:              namespaces,
		MetricsAddr:             opts.String("metrics-addr"),
		HealthAddr:              opts.String("health-addr"),
		Log:                     log,
	})
	if err != nil {
		return err
	}
	if opts.String("health-addr") != "" {
		if err := mgr.AddReadyzCheck("dex", dex.ReadyCheck(dexClient, 5*time.Second)); err != nil {
			return err
		}
	}

	resync := opts.Duration("resync-period")
	recorder := mgr.GetEventRecorder(controller.FieldManager)
	clients := &dexclient.Reconciler{
		Client:       mgr.GetClient(),
		APIReader:    mgr.GetAPIReader(),
		Scheme:       mgr.GetScheme(),
		Dex:          dexClient,
		Issuer:       opts.String("dex-issuer"),
		ResyncPeriod: resync,
		Recorder:     recorder,
		Log:          log.With(slog.String("controller", "dexclient")),
	}
	if err := clients.SetupWithManager(ctx, mgr); err != nil {
		return err
	}
	connectors := &dexconnector.Reconciler{
		Client:       mgr.GetClient(),
		APIReader:    mgr.GetAPIReader(),
		Dex:          dexClient,
		ResyncPeriod: resync,
		Recorder:     recorder,
		Log:          log.With(slog.String("controller", "dexconnector")),
	}
	if err := connectors.SetupWithManager(ctx, mgr); err != nil {
		return err
	}

	log.LogAttrs(ctx, slog.LevelInfo, "dex-client-controller starting",
		slog.String("version", version.Version),
		slog.String("dex_address", opts.String("dex-address")),
		slog.Any("watch_namespaces", namespaces),
		slog.Duration("resync_period", resync))
	return mgr.Start(ctx)
}
