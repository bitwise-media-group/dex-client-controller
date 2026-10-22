// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package v1alpha1_test

import (
	"context"
	"os"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	v1alpha1 "github.com/bitwise-media-group/dex-client-controller/api/v1alpha1"
)

// startEnv boots an envtest API server with the generated CRDs installed and
// returns a client against it. Skipped without KUBEBUILDER_ASSETS (hack/test.sh
// sets it; `mise run test` runs that).
func startEnv(t *testing.T) client.Client {
	t.Helper()
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; skipping envtest schema validation")
	}
	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{"../../config/crd"},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() {
		if err := env.Stop(); err != nil {
			t.Errorf("stop envtest: %v", err)
		}
	})
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	return c
}

func meta(name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: "default"}
}

func TestSchemaValidation(t *testing.T) {
	c := startEnv(t)
	ctx := context.Background()

	redirect := []string{"https://app.example.com/callback"}
	creates := []struct {
		name string
		obj  client.Object
		ok   bool
	}{
		{"confidential client", &v1alpha1.DexClient{
			ObjectMeta: meta("ok"), Spec: v1alpha1.DexClientSpec{RedirectURIs: redirect},
		}, true},
		{"confidential client needs redirectURIs", &v1alpha1.DexClient{
			ObjectMeta: meta("no-redirect"), Spec: v1alpha1.DexClientSpec{Name: "x"},
		}, false},
		{"public client needs no redirectURIs", &v1alpha1.DexClient{
			ObjectMeta: meta("public"), Spec: v1alpha1.DexClientSpec{Public: true},
		}, true},
		{"public client rejects secretRef", &v1alpha1.DexClient{
			ObjectMeta: meta("public-secret"), Spec: v1alpha1.DexClientSpec{
				Public: true, SecretRef: &v1alpha1.ClientSecretReference{Name: "s"},
			},
		}, false},
		{"public client rejects generatedSecret", &v1alpha1.DexClient{
			ObjectMeta: meta("public-generated"), Spec: v1alpha1.DexClientSpec{
				Public: true, GeneratedSecret: &v1alpha1.GeneratedSecret{Name: "s"},
			},
		}, false},
		{"secretRef and generatedSecret are exclusive", &v1alpha1.DexClient{
			ObjectMeta: meta("both"), Spec: v1alpha1.DexClientSpec{
				RedirectURIs:    redirect,
				SecretRef:       &v1alpha1.ClientSecretReference{Name: "s"},
				GeneratedSecret: &v1alpha1.GeneratedSecret{Name: "g"},
			},
		}, false},
		{"unknown refreshTokenLifetime", &v1alpha1.DexClient{
			ObjectMeta: meta("lifetime"), Spec: v1alpha1.DexClientSpec{
				RedirectURIs: redirect, RefreshTokenLifetime: "forever",
			},
		}, false},
		{"connector", &v1alpha1.DexConnector{
			ObjectMeta: meta("oidc"), Spec: v1alpha1.DexConnectorSpec{
				Type:   "oidc",
				Config: &apiextensionsv1.JSON{Raw: []byte(`{"issuer":"https://idp","nested":{"deep":[1,2]}}`)},
				SecretRefs: []v1alpha1.ConnectorSecretRef{{
					ConfigField: "nested.clientSecret",
					ValueFrom:   v1alpha1.SecretKeySelector{Name: "creds", Key: "secret"},
				}},
				GrantTypes: []string{"authorization_code", "urn:ietf:params:oauth:grant-type:device_code"},
			},
		}, true},
		{"connector needs a type", &v1alpha1.DexConnector{ObjectMeta: meta("no-type")}, false},
		{"connector config must be an object", &v1alpha1.DexConnector{
			ObjectMeta: meta("array-config"), Spec: v1alpha1.DexConnectorSpec{
				Type: "oidc", Config: &apiextensionsv1.JSON{Raw: []byte(`["a"]`)},
			},
		}, false},
		{"connector configField must be a dotted path", &v1alpha1.DexConnector{
			ObjectMeta: meta("bad-path"), Spec: v1alpha1.DexConnectorSpec{
				Type: "oidc",
				SecretRefs: []v1alpha1.ConnectorSecretRef{{
					ConfigField: "a..b", ValueFrom: v1alpha1.SecretKeySelector{Name: "c", Key: "k"},
				}},
			},
		}, false},
		{"connector unknown grant type", &v1alpha1.DexConnector{
			ObjectMeta: meta("bad-grant"), Spec: v1alpha1.DexConnectorSpec{
				Type: "oidc", GrantTypes: []string{"client_credentials"},
			},
		}, false},
	}
	for _, tt := range creates {
		t.Run(tt.name, func(t *testing.T) {
			err := c.Create(ctx, tt.obj)
			if (err == nil) != tt.ok {
				t.Errorf("Create = %v, want ok=%v", err, tt.ok)
			}
		})
	}

	t.Run("secretRef key defaults to clientSecret", func(t *testing.T) {
		obj := &v1alpha1.DexClient{ObjectMeta: meta("default-key"), Spec: v1alpha1.DexClientSpec{
			RedirectURIs: redirect, SecretRef: &v1alpha1.ClientSecretReference{Name: "s"},
		}}
		if err := c.Create(ctx, obj); err != nil {
			t.Fatal(err)
		}
		if obj.Spec.SecretRef.Key != "clientSecret" {
			t.Errorf("secretRef.key = %q, want the clientSecret default", obj.Spec.SecretRef.Key)
		}
	})

	t.Run("connector config survives unpruned", func(t *testing.T) {
		var got v1alpha1.DexConnector
		if err := c.Get(ctx, client.ObjectKey{Namespace: "default", Name: "oidc"}, &got); err != nil {
			t.Fatal(err)
		}
		if string(got.Spec.Config.Raw) != `{"issuer":"https://idp","nested":{"deep":[1,2]}}` {
			t.Errorf("config = %s, want it stored verbatim", got.Spec.Config.Raw)
		}
	})

	t.Run("clientID is immutable", func(t *testing.T) {
		obj := &v1alpha1.DexClient{ObjectMeta: meta("immutable"), Spec: v1alpha1.DexClientSpec{RedirectURIs: redirect}}
		if err := c.Create(ctx, obj); err != nil {
			t.Fatal(err)
		}
		obj.Spec.Name = "renamed is fine"
		if err := c.Update(ctx, obj); err != nil {
			t.Fatalf("Update(name) = %v, want nil", err)
		}
		obj.Spec.ClientID = "set-later"
		if err := c.Update(ctx, obj); err == nil {
			t.Error("Update(clientID set after creation) = nil, want CEL rejection")
		}
	})

	t.Run("connector id is immutable", func(t *testing.T) {
		obj := &v1alpha1.DexConnector{ObjectMeta: meta("immutable"), Spec: v1alpha1.DexConnectorSpec{
			Type: "github", ID: "github",
		}}
		if err := c.Create(ctx, obj); err != nil {
			t.Fatal(err)
		}
		obj.Spec.ID = ""
		if err := c.Update(ctx, obj); err == nil {
			t.Error("Update(id removed) = nil, want CEL rejection")
		}
	})
}
