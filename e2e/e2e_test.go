// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package e2e drives the real controller binary against a real dex and an
// envtest kube-apiserver. See harness_test.go for the environment.
package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	api "github.com/dexidp/dex/api/v2"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/bitwise-media-group/dex-client-controller/api/v1alpha1"
	"github.com/bitwise-media-group/dex-client-controller/internal/dex"
)

// ready returns nil once obj's Ready condition has the given status and
// reason for its current generation.
func (e *env) ready(obj client.Object, conds func() []metav1.Condition, status metav1.ConditionStatus,
	reason string) func() error {
	return func() error {
		if err := e.kube.Get(context.Background(), client.ObjectKeyFromObject(obj), obj); err != nil {
			return err
		}
		c := meta.FindStatusCondition(conds(), v1alpha1.ConditionReady)
		switch {
		case c == nil:
			return errors.New("no Ready condition yet")
		case c.ObservedGeneration != obj.GetGeneration():
			return fmt.Errorf("Ready describes generation %d, object is at %d", c.ObservedGeneration, obj.GetGeneration())
		case c.Status != status || c.Reason != reason:
			return fmt.Errorf("Ready = %s/%s (%s), want %s/%s", c.Status, c.Reason, c.Message, status, reason)
		}
		return nil
	}
}

func (e *env) clientReady(obj *v1alpha1.DexClient, status metav1.ConditionStatus, reason string) func() error {
	return e.ready(obj, func() []metav1.Condition { return obj.Status.Conditions }, status, reason)
}

func (e *env) connectorReady(obj *v1alpha1.DexConnector, status metav1.ConditionStatus, reason string) func() error {
	return e.ready(obj, func() []metav1.Condition { return obj.Status.Conditions }, status, reason)
}

// dexSecret returns the secret dex holds for a client.
func (e *env) dexSecret(id string) (string, error) {
	resp, err := e.dex.GetClient(context.Background(), &api.GetClientReq{Id: id})
	if err != nil {
		return "", err
	}
	return resp.GetClient().GetSecret(), nil
}

// gone returns nil once obj no longer exists.
func (e *env) gone(obj client.Object) func() error {
	return func() error {
		err := e.kube.Get(context.Background(), client.ObjectKeyFromObject(obj), obj)
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("%s still exists (finalizers %v)", obj.GetName(), obj.GetFinalizers())
	}
}

func TestDexClientGeneratedSecret(t *testing.T) {
	e := world(t)
	ctx := context.Background()
	obj := &v1alpha1.DexClient{
		ObjectMeta: metav1.ObjectMeta{Name: "grafana", Namespace: nsApps},
		Spec: v1alpha1.DexClientSpec{
			Name:         "Grafana",
			RedirectURIs: []string{"https://grafana.example.com/login/generic_oauth"},
		},
	}
	if err := e.kube.Create(ctx, obj); err != nil {
		t.Fatal(err)
	}
	e.eventually(t, "DexClient synced", e.clientReady(obj, metav1.ConditionTrue, v1alpha1.ReasonSynced))

	var s corev1.Secret
	if err := e.kube.Get(ctx, client.ObjectKey{Namespace: nsApps, Name: "grafana-dex-client"}, &s); err != nil {
		t.Fatalf("generated Secret: %v", err)
	}
	if string(s.Data["clientID"]) != "apps-grafana" || string(s.Data["issuer"]) != e.issuer {
		t.Errorf("generated Secret data = %q, want clientID apps-grafana and the issuer", s.Data)
	}
	got, err := e.dexSecret("apps-grafana")
	if err != nil {
		t.Fatalf("GetClient: %v", err)
	}
	if got == "" || got != string(s.Data["clientSecret"]) {
		t.Errorf("dex secret %q does not match the generated Secret's", got)
	}

	if err := e.kube.Delete(ctx, obj); err != nil {
		t.Fatal(err)
	}
	e.eventually(t, "DexClient released", e.gone(obj))
	if _, err := e.dex.GetClient(ctx, &api.GetClientReq{Id: "apps-grafana"}); !dex.IsNotFound(err) {
		t.Errorf("GetClient after delete = %v, want not found", err)
	}
}

func TestDexClientSecretRotation(t *testing.T) {
	e := world(t)
	ctx := context.Background()
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "app-oidc", Namespace: nsApps},
		Data:       map[string][]byte{"clientSecret": []byte("first")},
	}
	if err := e.kube.Create(ctx, s); err != nil {
		t.Fatal(err)
	}
	obj := &v1alpha1.DexClient{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: nsApps},
		Spec: v1alpha1.DexClientSpec{
			ClientID:     "app",
			RedirectURIs: []string{"https://app.example.com/callback"},
			SecretRef:    &v1alpha1.ClientSecretReference{Name: "app-oidc"},
		},
	}
	if err := e.kube.Create(ctx, obj); err != nil {
		t.Fatal(err)
	}
	e.eventually(t, "DexClient synced", e.clientReady(obj, metav1.ConditionTrue, v1alpha1.ReasonSynced))
	if got, err := e.dexSecret("app"); err != nil || got != "first" {
		t.Fatalf("dex secret = %q, %v; want first", got, err)
	}

	// Rotating the Secret alone (no CR change) reaches dex through the
	// metadata-only Secret watch; the 1h resync could not.
	s.Data["clientSecret"] = []byte("second")
	if err := e.kube.Update(ctx, s); err != nil {
		t.Fatal(err)
	}
	e.eventually(t, "rotated secret in dex", func() error {
		got, err := e.dexSecret("app")
		if err != nil {
			return err
		}
		if got != "second" {
			return fmt.Errorf("dex secret = %q, want second", got)
		}
		return nil
	})

	if err := e.kube.Delete(ctx, obj); err != nil {
		t.Fatal(err)
	}
	e.eventually(t, "DexClient released", e.gone(obj))
	if _, err := e.dex.GetClient(ctx, &api.GetClientReq{Id: "app"}); !dex.IsNotFound(err) {
		t.Errorf("GetClient after delete = %v, want not found", err)
	}
}

func TestDexClientStaticIsReadOnly(t *testing.T) {
	e := world(t)
	obj := &v1alpha1.DexClient{
		ObjectMeta: metav1.ObjectMeta{Name: "static", Namespace: nsApps},
		Spec: v1alpha1.DexClientSpec{
			ClientID:     staticClientID,
			RedirectURIs: []string{"https://elsewhere.example.com/callback"},
		},
	}
	if err := e.kube.Create(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	e.eventually(t, "static client refused", e.clientReady(obj, metav1.ConditionFalse, v1alpha1.ReasonReadOnly))
	if got, err := e.dexSecret(staticClientID); err != nil || got != "from-config" {
		t.Errorf("static client secret = %q, %v; want it untouched", got, err)
	}
	if err := e.kube.Delete(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	e.eventually(t, "ReadOnly DexClient released", e.gone(obj))
}

func TestDexConnector(t *testing.T) {
	e := world(t)
	ctx := context.Background()
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "idp-creds", Namespace: nsApps},
		Data:       map[string][]byte{"clientSecret": []byte("connector-secret")},
	}
	if err := e.kube.Create(ctx, s); err != nil {
		t.Fatal(err)
	}
	obj := &v1alpha1.DexConnector{
		ObjectMeta: metav1.ObjectMeta{Name: "idp", Namespace: nsApps},
		Spec: v1alpha1.DexConnectorSpec{
			Type: "oidc",
			Name: "Example IdP",
			Config: &apiextensionsv1.JSON{
				Raw: []byte(`{"issuer":"https://idp.example.com","clientID":"dex","redirectURI":"https://dex/callback"}`),
			},
			SecretRefs: []v1alpha1.ConnectorSecretRef{{
				ConfigField: "clientSecret",
				ValueFrom:   v1alpha1.SecretKeySelector{Name: "idp-creds", Key: "clientSecret"},
			}},
		},
	}
	if err := e.kube.Create(ctx, obj); err != nil {
		t.Fatal(err)
	}
	e.eventually(t, "DexConnector synced", e.connectorReady(obj, metav1.ConditionTrue, v1alpha1.ReasonSynced))

	connector := func() (*api.Connector, error) {
		list, err := e.dex.ListConnectors(ctx, &api.ListConnectorReq{})
		if err != nil {
			return nil, err
		}
		for _, c := range list.GetConnectors() {
			if c.GetId() == "apps-idp" {
				return c, nil
			}
		}
		return nil, nil
	}
	c, err := connector()
	if err != nil || c == nil {
		t.Fatalf("ListConnectors: connector %v, err %v", c, err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(c.Config, &cfg); err != nil {
		t.Fatalf("connector config %s: %v", c.Config, err)
	}
	if cfg["clientSecret"] != "connector-secret" || cfg["issuer"] != "https://idp.example.com" {
		t.Errorf("connector config = %s, want the spec plus the Secret's clientSecret", c.Config)
	}
	if c.Type != "oidc" || c.Name != "Example IdP" {
		t.Errorf("connector = %s/%s, want oidc/Example IdP", c.Type, c.Name)
	}

	if err := e.kube.Delete(ctx, obj); err != nil {
		t.Fatal(err)
	}
	e.eventually(t, "DexConnector released", e.gone(obj))
	if c, err := connector(); err != nil || c != nil {
		t.Errorf("connector after delete = %v, %v; want it gone", c, err)
	}
}

func TestFeatureDisabled(t *testing.T) {
	e := world(t)
	ctx := context.Background()
	conn := &v1alpha1.DexConnector{
		ObjectMeta: metav1.ObjectMeta{Name: "github", Namespace: nsNoFlag},
		Spec:       v1alpha1.DexConnectorSpec{Type: "github"},
	}
	if err := e.kube.Create(ctx, conn); err != nil {
		t.Fatal(err)
	}
	e.eventually(t, "connector refused by a dex without the flag",
		e.connectorReady(conn, metav1.ConditionFalse, v1alpha1.ReasonFeatureDisabled))

	// Clients need no flag: the same dex still takes them.
	cl := &v1alpha1.DexClient{
		ObjectMeta: metav1.ObjectMeta{Name: "cli", Namespace: nsNoFlag},
		Spec:       v1alpha1.DexClientSpec{Public: true},
	}
	if err := e.kube.Create(ctx, cl); err != nil {
		t.Fatal(err)
	}
	e.eventually(t, "client synced on the flagless dex", e.clientReady(cl, metav1.ConditionTrue, v1alpha1.ReasonSynced))
}
