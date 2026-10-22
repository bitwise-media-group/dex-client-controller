// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package dexconnector

import (
	"context"
	"log/slog"
	"slices"
	"testing"
	"time"

	api "github.com/dexidp/dex/api/v2"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	v1alpha1 "github.com/bitwise-media-group/dex-client-controller/api/v1alpha1"
	"github.com/bitwise-media-group/dex-client-controller/internal/controller"
	"github.com/bitwise-media-group/dex-client-controller/internal/dex/dextest"
	"github.com/bitwise-media-group/dex-client-controller/internal/kube"
)

const ns = "platform"

var t0 = metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

type harness struct {
	r   *Reconciler
	c   client.Client
	dex *dextest.Server
}

func newHarness(t *testing.T, objs ...client.Object) *harness {
	t.Helper()
	c := fake.NewClientBuilder().
		WithScheme(kube.Scheme()).
		WithObjects(objs...).
		WithStatusSubresource(&v1alpha1.DexConnector{}).
		WithIndex(&v1alpha1.DexConnector{}, controller.IndexDexID, IndexDexID).
		WithIndex(&v1alpha1.DexConnector{}, controller.IndexSecretName, IndexSecretName).
		Build()
	d := dextest.New()
	return &harness{
		r: &Reconciler{
			Client:       c,
			APIReader:    c,
			Dex:          d,
			ResyncPeriod: 5 * time.Minute,
			Log:          slog.New(slog.DiscardHandler),
		},
		c:   c,
		dex: d,
	}
}

func (h *harness) reconcile(t *testing.T, name string) (ctrl.Result, error) {
	t.Helper()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}}
	return h.r.Reconcile(context.Background(), req)
}

func (h *harness) mustReconcile(t *testing.T, name string) {
	t.Helper()
	res, err := h.reconcile(t, name)
	if err != nil {
		t.Fatalf("Reconcile(%s): %v", name, err)
	}
	if res.RequeueAfter != h.r.ResyncPeriod {
		t.Errorf("RequeueAfter = %v, want the resync period %v", res.RequeueAfter, h.r.ResyncPeriod)
	}
}

func (h *harness) get(t *testing.T, name string) *v1alpha1.DexConnector {
	t.Helper()
	var obj v1alpha1.DexConnector
	if err := h.c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, &obj); err != nil {
		t.Fatalf("get DexConnector %s: %v", name, err)
	}
	return &obj
}

func (h *harness) update(t *testing.T, name string, mutate func(*v1alpha1.DexConnector)) {
	t.Helper()
	obj := h.get(t, name)
	mutate(obj)
	if err := h.c.Update(context.Background(), obj); err != nil {
		t.Fatalf("update DexConnector %s: %v", name, err)
	}
}

// setSecret creates or updates the connector's credentials Secret.
func (h *harness) setSecret(t *testing.T, value string) {
	const name, key = "azure-ad-creds", "clientSecret"
	t.Helper()
	ctx := context.Background()
	var s corev1.Secret
	err := h.c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &s)
	switch {
	case apierrors.IsNotFound(err):
		s = corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Data:       map[string][]byte{key: []byte(value)},
		}
		err = h.c.Create(ctx, &s)
	case err == nil:
		s.Data[key] = []byte(value)
		err = h.c.Update(ctx, &s)
	}
	if err != nil {
		t.Fatalf("set Secret %s: %v", name, err)
	}
}

func assertReady(t *testing.T, obj *v1alpha1.DexConnector, status metav1.ConditionStatus, reason string) {
	t.Helper()
	c := meta.FindStatusCondition(obj.Status.Conditions, v1alpha1.ConditionReady)
	if c == nil {
		t.Fatalf("%s: no Ready condition", obj.Name)
	}
	if c.Status != status || c.Reason != reason {
		t.Fatalf("%s: Ready = %s/%s (%s), want %s/%s", obj.Name, c.Status, c.Reason, c.Message, status, reason)
	}
}

func azure(mutate ...func(*v1alpha1.DexConnector)) *v1alpha1.DexConnector {
	obj := &v1alpha1.DexConnector{
		ObjectMeta: metav1.ObjectMeta{Name: "azure", Namespace: ns, CreationTimestamp: t0},
		Spec: v1alpha1.DexConnectorSpec{
			Type: "oidc",
			Name: "Azure AD",
			Config: &apiextensionsv1.JSON{
				Raw: []byte(`{"issuer":"https://login.microsoftonline.com/x/v2.0","clientID":"abc","getUserInfo":true}`),
			},
			SecretRefs: []v1alpha1.ConnectorSecretRef{{
				ConfigField: "clientSecret",
				ValueFrom:   v1alpha1.SecretKeySelector{Name: "azure-ad-creds", Key: "clientSecret"},
			}},
		},
	}
	for _, m := range mutate {
		m(obj)
	}
	return obj
}

func TestCreateAndUpdate(t *testing.T) {
	h := newHarness(t, azure())

	h.mustReconcile(t, "azure")
	assertReady(t, h.get(t, "azure"), metav1.ConditionFalse, v1alpha1.ReasonSecretNotFound)
	if c, _ := h.dex.Connector("platform-azure"); c != nil {
		t.Fatal("connector created without its secret")
	}

	h.setSecret(t, "s3cret")
	h.mustReconcile(t, "azure")
	obj := h.get(t, "azure")
	assertReady(t, obj, metav1.ConditionTrue, v1alpha1.ReasonSynced)
	if obj.Status.ID != "platform-azure" {
		t.Errorf("status.id = %q, want platform-azure", obj.Status.ID)
	}
	if !controllerutil.ContainsFinalizer(obj, v1alpha1.Finalizer) {
		t.Error("finalizer not added")
	}
	c, rv := h.dex.Connector("platform-azure")
	const wantCfg = `{"clientID":"abc","clientSecret":"s3cret","getUserInfo":true,` +
		`"issuer":"https://login.microsoftonline.com/x/v2.0"}`
	if c.Type != "oidc" || c.Name != "Azure AD" || string(c.Config) != wantCfg {
		t.Fatalf("dex connector = %v (config %s)", c, c.Config)
	}

	// No-op reconciles leave the resource version alone, so dex does not
	// rebuild the connector.
	h.mustReconcile(t, "azure")
	if _, got := h.dex.Connector("platform-azure"); got != rv || h.dex.Calls("UpdateConnector") != 0 {
		t.Errorf("resource version %d -> %d, UpdateConnector=%d on a no-op", rv, got, h.dex.Calls("UpdateConnector"))
	}

	// Rotating the secret updates the config in place.
	h.setSecret(t, "rotated")
	h.mustReconcile(t, "azure")
	c, rv2 := h.dex.Connector("platform-azure")
	const rotatedCfg = `{"clientID":"abc","clientSecret":"rotated","getUserInfo":true,` +
		`"issuer":"https://login.microsoftonline.com/x/v2.0"}`
	if string(c.Config) != rotatedCfg || rv2 != rv+1 {
		t.Errorf("after rotation: config %s, resource version %d (was %d)", c.Config, rv2, rv)
	}

	// Spec changes: name, type, grant types.
	h.update(t, "azure", func(o *v1alpha1.DexConnector) {
		o.Spec.Name = "Entra ID"
		o.Spec.Type = "microsoft"
		o.Spec.GrantTypes = []string{"authorization_code", "refresh_token"}
	})
	h.mustReconcile(t, "azure")
	c, _ = h.dex.Connector("platform-azure")
	wantGrants := []string{"authorization_code", "refresh_token"}
	if c.Name != "Entra ID" || c.Type != "microsoft" || !slices.Equal(c.GrantTypes, wantGrants) {
		t.Errorf("after spec change: %v", c)
	}

	// Dropping the grant types restores an unrestricted connector.
	h.update(t, "azure", func(o *v1alpha1.DexConnector) { o.Spec.GrantTypes = nil })
	h.mustReconcile(t, "azure")
	if c, _ = h.dex.Connector("platform-azure"); len(c.GrantTypes) != 0 {
		t.Errorf("grant types = %v, want unrestricted", c.GrantTypes)
	}
}

func TestDefaults(t *testing.T) {
	h := newHarness(t, &v1alpha1.DexConnector{
		ObjectMeta: metav1.ObjectMeta{Name: "github", Namespace: ns, CreationTimestamp: t0},
		Spec:       v1alpha1.DexConnectorSpec{Type: "github", ID: "github"},
	})
	h.mustReconcile(t, "github")
	assertReady(t, h.get(t, "github"), metav1.ConditionTrue, v1alpha1.ReasonSynced)
	c, _ := h.dex.Connector("github")
	if c == nil || c.Name != "github" || string(c.Config) != "{}" {
		t.Errorf("dex connector = %v, want name defaulted to the object name and an empty config", c)
	}
}

func TestDriftRepair(t *testing.T) {
	h := newHarness(t, azure())
	h.setSecret(t, "s3cret")
	h.mustReconcile(t, "azure")

	// Someone edits the connector behind the controller's back.
	if _, err := h.dex.UpdateConnector(context.Background(), &api.UpdateConnectorReq{
		Id: "platform-azure", NewConfig: []byte(`{"issuer":"https://evil"}`),
	}); err != nil {
		t.Fatal(err)
	}
	h.mustReconcile(t, "azure")
	const wantCfg = `{"clientID":"abc","clientSecret":"s3cret","getUserInfo":true,` +
		`"issuer":"https://login.microsoftonline.com/x/v2.0"}`
	if c, _ := h.dex.Connector("platform-azure"); string(c.Config) != wantCfg {
		t.Errorf("config not restored: %s", c.Config)
	}

	h.dex.Wipe()
	h.mustReconcile(t, "azure")
	if c, _ := h.dex.Connector("platform-azure"); c == nil {
		t.Error("connector not recreated after dex lost it")
	}
}

func TestInvalidConfig(t *testing.T) {
	h := newHarness(t, azure(func(o *v1alpha1.DexConnector) {
		o.Spec.Config = &apiextensionsv1.JSON{Raw: []byte(`{"clientSecret":{"nested":true},"bind":"x"}`)}
		o.Spec.SecretRefs[0].ConfigField = "bind.pw"
	}))
	h.setSecret(t, "s3cret")
	h.mustReconcile(t, "azure")
	assertReady(t, h.get(t, "azure"), metav1.ConditionFalse, v1alpha1.ReasonInvalidConfig)
}

func TestFeatureDisabled(t *testing.T) {
	h := newHarness(t, azure())
	h.setSecret(t, "s3cret")
	h.dex.ConnectorsCRUD = false
	h.mustReconcile(t, "azure")
	obj := h.get(t, "azure")
	assertReady(t, obj, metav1.ConditionFalse, v1alpha1.ReasonFeatureDisabled)
	const wantMsg = "dex refuses connector changes: run dex with DEX_API_CONNECTORS_CRUD=true"
	if c := meta.FindStatusCondition(obj.Status.Conditions, v1alpha1.ConditionReady); c.Message != wantMsg {
		t.Errorf("message = %q, want it to name the flag", c.Message)
	}

	// Deleting it holds the finalizer: dex might still serve the connector.
	if err := h.c.Delete(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if _, err := h.reconcile(t, "azure"); err == nil {
		t.Fatal("finalize with the flag off returned nil")
	}
	if !controllerutil.ContainsFinalizer(h.get(t, "azure"), v1alpha1.Finalizer) {
		t.Fatal("finalizer removed although dex refused the delete")
	}
	h.dex.ConnectorsCRUD = true
	if _, err := h.reconcile(t, "azure"); err != nil {
		t.Fatalf("finalize once the flag is back: %v", err)
	}
}

func TestReadOnly(t *testing.T) {
	h := newHarness(t, azure(func(o *v1alpha1.DexConnector) { o.Spec.ID = "static" }))
	h.dex.AddStaticConnector(&api.Connector{Id: "static", Type: "mockCallback", Name: "Mock", Config: []byte(`{}`)})
	h.setSecret(t, "s3cret")
	h.mustReconcile(t, "azure")
	assertReady(t, h.get(t, "azure"), metav1.ConditionFalse, v1alpha1.ReasonReadOnly)

	if err := h.c.Delete(context.Background(), h.get(t, "azure")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.reconcile(t, "azure"); err != nil {
		t.Fatalf("finalize a ReadOnly connector: %v", err)
	}
}

func TestConflictAndDelete(t *testing.T) {
	h := newHarness(t,
		azure(func(o *v1alpha1.DexConnector) { o.Spec.ID = "azure" }),
		azure(func(o *v1alpha1.DexConnector) {
			o.Name = "azure-copy"
			o.Spec.ID = "azure"
			o.CreationTimestamp = metav1.NewTime(t0.Add(time.Second))
		}),
	)
	h.setSecret(t, "s3cret")
	h.mustReconcile(t, "azure-copy")
	assertReady(t, h.get(t, "azure-copy"), metav1.ConditionFalse, v1alpha1.ReasonConflict)
	h.mustReconcile(t, "azure")
	assertReady(t, h.get(t, "azure"), metav1.ConditionTrue, v1alpha1.ReasonSynced)

	if reqs := h.r.forSecret(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "azure-ad-creds", Namespace: ns},
	}); len(reqs) != 2 {
		t.Errorf("forSecret = %v, want both connectors", reqs)
	}
	if reqs := h.r.sameID(context.Background(), h.get(t, "azure")); len(reqs) != 1 || reqs[0].Name != "azure-copy" {
		t.Errorf("sameID = %v, want azure-copy", reqs)
	}

	// Deleting the loser leaves the connector; deleting the winner too
	// removes it.
	h.mustReconcile(t, "azure-copy")
	if err := h.c.Delete(context.Background(), h.get(t, "azure-copy")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.reconcile(t, "azure-copy"); err != nil {
		t.Fatal(err)
	}
	if c, _ := h.dex.Connector("azure"); c == nil {
		t.Fatal("deleting the losing claimant removed the connector")
	}
	if err := h.c.Delete(context.Background(), h.get(t, "azure")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.reconcile(t, "azure"); err != nil {
		t.Fatal(err)
	}
	if c, _ := h.dex.Connector("azure"); c != nil {
		t.Error("connector survived its DexConnector's deletion")
	}
	var obj v1alpha1.DexConnector
	err := h.c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "azure"}, &obj)
	if !apierrors.IsNotFound(err) {
		t.Errorf("object still present after finalize (err %v)", err)
	}
}

func TestDexUnavailable(t *testing.T) {
	h := newHarness(t, azure())
	h.setSecret(t, "s3cret")
	h.dex.Err = dextest.ErrUnavailable
	if _, err := h.reconcile(t, "azure"); err == nil {
		t.Fatal("Reconcile with dex down returned nil, want a retry")
	}
	assertReady(t, h.get(t, "azure"), metav1.ConditionFalse, v1alpha1.ReasonDexUnavailable)
}

func TestDexRejected(t *testing.T) {
	h := newHarness(t, azure(func(o *v1alpha1.DexConnector) {
		o.Spec.GrantTypes = []string{"client_credentials"} // not restrictable per connector
	}))
	h.setSecret(t, "s3cret")
	h.mustReconcile(t, "azure")
	assertReady(t, h.get(t, "azure"), metav1.ConditionFalse, v1alpha1.ReasonDexRejected)
}
