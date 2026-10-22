// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package kube

import (
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TestScheme covers the kinds the controllers build clients for; a scheme
// missing one of them fails at first use with an unhelpful "no kind
// registered".
func TestScheme(t *testing.T) {
	s := Scheme()
	for _, gvk := range []schema.GroupVersionKind{
		{Group: "dex.bitwisemedia.uk", Version: "v1alpha1", Kind: "DexClient"},
		{Group: "dex.bitwisemedia.uk", Version: "v1alpha1", Kind: "DexConnector"},
		{Version: "v1", Kind: "Secret"},
		{Group: "coordination.k8s.io", Version: "v1", Kind: "Lease"},
	} {
		if !s.Recognizes(gvk) {
			t.Errorf("scheme does not recognize %s", gvk)
		}
	}
}

func TestRestConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	const kubeconfig = `apiVersion: v1
kind: Config
current-context: dev
clusters:
  - name: dev
    cluster:
      server: https://dev.example.test:6443
contexts:
  - name: dev
    context:
      cluster: dev
      user: dev
users:
  - name: dev
    user:
      token: dev-token
`
	if err := os.WriteFile(path, []byte(kubeconfig), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	cfg, err := RestConfig(path)
	if err != nil {
		t.Fatalf("RestConfig: %v", err)
	}
	if cfg.Host != "https://dev.example.test:6443" {
		t.Errorf("host = %q, want the kubeconfig cluster", cfg.Host)
	}
	if cfg.QPS != -1 {
		t.Errorf("QPS = %v, want -1 (client-side limiter disabled)", cfg.QPS)
	}
	if _, err := RestConfig(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("RestConfig accepted a missing kubeconfig path")
	}
}

func TestBindAddr(t *testing.T) {
	if got := bindAddr(""); got != "0" {
		t.Errorf(`bindAddr("") = %q, want "0"`, got)
	}
	if got := bindAddr(":8080"); got != ":8080" {
		t.Errorf(`bindAddr(":8080") = %q, want ":8080"`, got)
	}
}
