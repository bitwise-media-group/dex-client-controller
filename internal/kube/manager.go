// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package kube

import (
	"fmt"
	"log/slog"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	v1alpha1 "github.com/bitwise-media-group/dex-client-controller/api/v1alpha1"
)

// Options configure a controller manager.
type Options struct {
	// Kubeconfig is the path to a kubeconfig file; empty means in-cluster
	// config (with the usual KUBECONFIG/service-account fallbacks).
	Kubeconfig string
	// LeaderElectionID is the Lease name; empty disables leader election.
	// The Deployment stays replicas:1 + Recreate — the lease is cheap
	// insurance against two replicas racing during a botched rollout.
	LeaderElectionID string
	// LeaderElectionNamespace holds the Lease; required when running outside
	// the cluster (in-cluster it defaults to the pod's namespace).
	LeaderElectionNamespace string
	// Namespaces scopes the cache; empty watches every namespace.
	Namespaces []string
	// MetricsAddr serves controller-runtime's Prometheus metrics; empty
	// disables the endpoint.
	MetricsAddr string
	// HealthAddr serves /healthz and /readyz for kubelet probes; empty
	// disables the endpoint.
	HealthAddr string
	// Log is bridged to controller-runtime's logr. nil discards.
	Log *slog.Logger
}

// Scheme returns a runtime scheme holding the client-go kinds and the
// dex.bitwisemedia.uk API group.
func Scheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(v1alpha1.AddToScheme(s))
	return s
}

// RestConfig resolves the REST config from an explicit kubeconfig path or
// the standard in-cluster/KUBECONFIG chain.
func RestConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("kubeconfig %s: %w", kubeconfig, err)
		}
		if cfg.QPS == 0 {
			// Match ctrl.GetConfig's in-cluster behavior: disable the
			// client-side rate limiter and rely on API priority and fairness.
			cfg.QPS = -1
		}
		return cfg, nil
	}
	cfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("kubernetes config: %w", err)
	}
	return cfg, nil
}

// NewManager builds a controller manager per Options. Secrets are never
// cached: their contents are read on demand through the API reader, and the
// controllers watch them metadata-only, so client secrets never sit in the
// controller's memory longer than one reconcile.
func NewManager(opts Options) (ctrl.Manager, error) {
	log := opts.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	ctrl.SetLogger(logr.FromSlogHandler(log.Handler()))

	cfg, err := RestConfig(opts.Kubeconfig)
	if err != nil {
		return nil, err
	}

	mgrOpts := ctrl.Options{
		Scheme:                 Scheme(),
		Metrics:                metricsserver.Options{BindAddress: bindAddr(opts.MetricsAddr)},
		HealthProbeBindAddress: opts.HealthAddr,
		Client: client.Options{
			Cache: &client.CacheOptions{DisableFor: []client.Object{&corev1.Secret{}}},
		},
		// managedFields are server-side-apply bookkeeping nothing here reads.
		Cache: cache.Options{DefaultTransform: cache.TransformStripManagedFields()},
	}
	if opts.LeaderElectionID != "" {
		mgrOpts.LeaderElection = true
		mgrOpts.LeaderElectionID = opts.LeaderElectionID
		mgrOpts.LeaderElectionNamespace = opts.LeaderElectionNamespace
		// Step down promptly on SIGTERM so a Recreate rollout's new pod
		// does not wait out the lease.
		mgrOpts.LeaderElectionReleaseOnCancel = true
	}
	if len(opts.Namespaces) > 0 {
		nss := make(map[string]cache.Config, len(opts.Namespaces))
		for _, ns := range opts.Namespaces {
			nss[ns] = cache.Config{}
		}
		mgrOpts.Cache.DefaultNamespaces = nss
	}

	mgr, err := ctrl.NewManager(cfg, mgrOpts)
	if err != nil {
		return nil, fmt.Errorf("build manager: %w", err)
	}
	if opts.HealthAddr != "" {
		if err := mgr.AddHealthzCheck("ping", healthz.Ping); err != nil {
			return nil, fmt.Errorf("add healthz check: %w", err)
		}
	}
	return mgr, nil
}

// bindAddr maps the empty MetricsAddr to controller-runtime's "disabled"
// sentinel.
func bindAddr(addr string) string {
	if addr == "" {
		return "0"
	}
	return addr
}
