// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package dexclient

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"
	"time"

	api "github.com/dexidp/dex/api/v2"
	corev1 "k8s.io/api/core/v1"
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

const (
	ns     = "monitoring"
	issuer = "https://dex.example.com"
)

var t0 = metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

type harness struct {
	r   *Reconciler
	c   client.Client
	dex *dextest.Server
}

func newHarness(t *testing.T, objs ...client.Object) *harness {
	t.Helper()
	scheme := kube.Scheme()
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&v1alpha1.DexClient{}).
		WithIndex(&v1alpha1.DexClient{}, controller.IndexDexID, IndexDexID).
		WithIndex(&v1alpha1.DexClient{}, controller.IndexSecretName, IndexSecretName).
		Build()
	d := dextest.New()
	return &harness{
		r: &Reconciler{
			Client:       c,
			APIReader:    c,
			Scheme:       scheme,
			Dex:          d,
			Issuer:       issuer,
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

func (h *harness) get(t *testing.T, name string) *v1alpha1.DexClient {
	t.Helper()
	var obj v1alpha1.DexClient
	if err := h.c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, &obj); err != nil {
		t.Fatalf("get DexClient %s: %v", name, err)
	}
	return &obj
}

func (h *harness) update(t *testing.T, name string, mutate func(*v1alpha1.DexClient)) {
	t.Helper()
	obj := h.get(t, name)
	mutate(obj)
	if err := h.c.Update(context.Background(), obj); err != nil {
		t.Fatalf("update DexClient %s: %v", name, err)
	}
}

func (h *harness) secret(t *testing.T, name string) *corev1.Secret {
	t.Helper()
	var s corev1.Secret
	if err := h.c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, &s); err != nil {
		t.Fatalf("get Secret %s: %v", name, err)
	}
	return &s
}

func assertReady(t *testing.T, obj *v1alpha1.DexClient, status metav1.ConditionStatus, reason string) {
	t.Helper()
	c := meta.FindStatusCondition(obj.Status.Conditions, v1alpha1.ConditionReady)
	if c == nil {
		t.Fatalf("%s: no Ready condition", obj.Name)
	}
	if c.Status != status || c.Reason != reason {
		t.Fatalf("%s: Ready = %s/%s (%s), want %s/%s", obj.Name, c.Status, c.Reason, c.Message, status, reason)
	}
	if c.ObservedGeneration != obj.Generation {
		t.Errorf("%s: Ready observedGeneration = %d, want %d", obj.Name, c.ObservedGeneration, obj.Generation)
	}
}

func dexClient(name string, mutate ...func(*v1alpha1.DexClient)) *v1alpha1.DexClient {
	obj := &v1alpha1.DexClient{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, CreationTimestamp: t0},
		Spec: v1alpha1.DexClientSpec{
			Name:         "Grafana",
			RedirectURIs: []string{"https://grafana.example.com/login/generic_oauth"},
		},
	}
	for _, m := range mutate {
		m(obj)
	}
	return obj
}

func secret(name string, data map[string]string) *corev1.Secret {
	s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}, Data: map[string][]byte{}}
	for k, v := range data {
		s.Data[k] = []byte(v)
	}
	return s
}

func withSecretRef(name, key string) func(*v1alpha1.DexClient) {
	return func(o *v1alpha1.DexClient) { o.Spec.SecretRef = &v1alpha1.ClientSecretReference{Name: name, Key: key} }
}

func TestGeneratedSecret(t *testing.T) {
	h := newHarness(t, dexClient("grafana"))
	h.mustReconcile(t, "grafana")

	obj := h.get(t, "grafana")
	assertReady(t, obj, metav1.ConditionTrue, v1alpha1.ReasonSynced)
	if obj.Status.ClientID != "monitoring-grafana" || obj.Status.SecretName != "grafana-dex-client" {
		t.Errorf("status = %+v, want clientID monitoring-grafana and secretName grafana-dex-client", obj.Status)
	}
	if !controllerutil.ContainsFinalizer(obj, v1alpha1.Finalizer) {
		t.Error("finalizer not added")
	}

	s := h.secret(t, "grafana-dex-client")
	if !metav1.IsControlledBy(s, obj) {
		t.Error("generated Secret is not controlled by the DexClient")
	}
	value := string(s.Data["clientSecret"])
	if len(value) < 40 {
		t.Errorf("generated secret %q is too short", value)
	}
	if string(s.Data["clientID"]) != "monitoring-grafana" || string(s.Data["issuer"]) != issuer {
		t.Errorf("Secret data = %q, want clientID and issuer keys", s.Data)
	}
	dc := h.dex.Client("monitoring-grafana")
	if dc == nil {
		t.Fatal("client not created in dex")
	}
	if dc.Secret != value || dc.Name != "Grafana" || !slices.Equal(dc.RedirectUris, obj.Spec.RedirectURIs) {
		t.Errorf("dex client = %v, want the spec and the generated secret", dc)
	}

	// A second pass changes nothing: the secret is stable and dex untouched.
	h.dex.ResetCalls()
	h.mustReconcile(t, "grafana")
	if got := string(h.secret(t, "grafana-dex-client").Data["clientSecret"]); got != value {
		t.Error("generated secret changed on a no-op reconcile")
	}
	for _, rpc := range []string{"CreateClient", "UpdateClient", "DeleteClient"} {
		if n := h.dex.Calls(rpc); n != 0 {
			t.Errorf("%s called %d times on a no-op reconcile", rpc, n)
		}
	}

	// A wiped key is refilled with a fresh secret, which dex then receives.
	s = h.secret(t, "grafana-dex-client")
	delete(s.Data, "clientSecret")
	if err := h.c.Update(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	h.mustReconcile(t, "grafana")
	refilled := string(h.secret(t, "grafana-dex-client").Data["clientSecret"])
	if refilled == "" || refilled == value {
		t.Fatalf("clientSecret after wipe = %q, want a new value", refilled)
	}
	if got := h.dex.Client("monitoring-grafana").Secret; got != refilled {
		t.Errorf("dex secret = %q, want the refilled %q", got, refilled)
	}
}

func TestGeneratedSecretNameTaken(t *testing.T) {
	h := newHarness(t,
		dexClient("grafana", func(o *v1alpha1.DexClient) {
			o.Spec.GeneratedSecret = &v1alpha1.GeneratedSecret{Name: "someone-elses"}
		}),
		secret("someone-elses", map[string]string{"clientSecret": "x"}),
	)
	h.mustReconcile(t, "grafana")
	assertReady(t, h.get(t, "grafana"), metav1.ConditionFalse, v1alpha1.ReasonInvalidConfig)
	if h.dex.Client("monitoring-grafana") != nil {
		t.Error("client created in dex despite the invalid config")
	}
}

func TestSecretRef(t *testing.T) {
	h := newHarness(t, dexClient("grafana", withSecretRef("grafana-oidc", "clientSecret")))

	// Missing Secret: reported, nothing created.
	h.mustReconcile(t, "grafana")
	assertReady(t, h.get(t, "grafana"), metav1.ConditionFalse, v1alpha1.ReasonSecretNotFound)
	if h.dex.Client("monitoring-grafana") != nil {
		t.Fatal("client created without its secret")
	}

	// Secret without the key: still SecretNotFound.
	if err := h.c.Create(context.Background(), secret("grafana-oidc", map[string]string{"other": "x"})); err != nil {
		t.Fatal(err)
	}
	h.mustReconcile(t, "grafana")
	assertReady(t, h.get(t, "grafana"), metav1.ConditionFalse, v1alpha1.ReasonSecretNotFound)

	// The key arrives: synced with exactly that value, nothing generated.
	s := h.secret(t, "grafana-oidc")
	s.Data["clientSecret"] = []byte("s3cret")
	if err := h.c.Update(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	h.mustReconcile(t, "grafana")
	obj := h.get(t, "grafana")
	assertReady(t, obj, metav1.ConditionTrue, v1alpha1.ReasonSynced)
	if obj.Status.SecretName != "grafana-oidc" {
		t.Errorf("status.secretName = %q, want grafana-oidc", obj.Status.SecretName)
	}
	if got := h.dex.Client("monitoring-grafana").Secret; got != "s3cret" {
		t.Errorf("dex secret = %q, want s3cret", got)
	}
	var gen corev1.Secret
	err := h.c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "grafana-dex-client"}, &gen)
	if !apierrors.IsNotFound(err) {
		t.Errorf("generated Secret exists alongside secretRef (err %v)", err)
	}

	// Rotation: a new value replaces the client (UpdateClient cannot).
	h.dex.ResetCalls()
	s = h.secret(t, "grafana-oidc")
	s.Data["clientSecret"] = []byte("rotated")
	if err := h.c.Update(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	h.mustReconcile(t, "grafana")
	if got := h.dex.Client("monitoring-grafana").Secret; got != "rotated" {
		t.Errorf("dex secret after rotation = %q, want rotated", got)
	}
	if h.dex.Calls("DeleteClient") != 1 || h.dex.Calls("CreateClient") != 1 {
		t.Errorf("rotation: DeleteClient=%d CreateClient=%d, want 1 and 1",
			h.dex.Calls("DeleteClient"), h.dex.Calls("CreateClient"))
	}
}

func TestUpdateInPlace(t *testing.T) {
	h := newHarness(t,
		dexClient("grafana", withSecretRef("grafana-oidc", "clientSecret"), func(o *v1alpha1.DexClient) {
			o.Spec.BackchannelLogoutURI = "https://grafana.example.com/logout"
			o.Spec.AllowedConnectors = []string{"github"}
		}),
		secret("grafana-oidc", map[string]string{"clientSecret": "s3cret"}),
	)
	h.mustReconcile(t, "grafana")

	h.dex.ResetCalls()
	h.update(t, "grafana", func(o *v1alpha1.DexClient) {
		o.Spec.RedirectURIs = append(o.Spec.RedirectURIs, "https://grafana2.example.com/cb")
		o.Spec.AllowedConnectors = []string{"github", "monitoring-azure"}
		o.Spec.TrustedPeers = []string{"kubectl"}
		o.Spec.BackchannelLogoutURI = "" // optional field: clearable in place
		o.Spec.RefreshTokenLifetime = v1alpha1.RefreshTokenLifetimeSession
		o.Spec.LogoURL = "https://grafana.example.com/logo.svg"
	})
	h.mustReconcile(t, "grafana")

	if h.dex.Calls("UpdateClient") != 1 || h.dex.Calls("DeleteClient") != 0 {
		t.Errorf("UpdateClient=%d DeleteClient=%d, want an in-place update",
			h.dex.Calls("UpdateClient"), h.dex.Calls("DeleteClient"))
	}
	dc := h.dex.Client("monitoring-grafana")
	switch {
	case len(dc.RedirectUris) != 2,
		!slices.Equal(dc.AllowedConnectors, []string{"github", "monitoring-azure"}),
		!slices.Equal(dc.TrustedPeers, []string{"kubectl"}),
		dc.BackchannelLogoutUri != "",
		dc.RefreshTokenLifetime != "session",
		dc.LogoUrl != "https://grafana.example.com/logo.svg",
		dc.Secret != "s3cret":
		t.Errorf("dex client after update = %v", dc)
	}
	obj := h.get(t, "grafana")
	assertReady(t, obj, metav1.ConditionTrue, v1alpha1.ReasonSynced)
}

func TestRecreateWhenUpdateCannot(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*v1alpha1.DexClient)
	}{
		{"clear allowedConnectors", func(o *v1alpha1.DexClient) { o.Spec.AllowedConnectors = nil }},
		{"clear trustedPeers", func(o *v1alpha1.DexClient) { o.Spec.TrustedPeers = nil }},
		{"clear name", func(o *v1alpha1.DexClient) { o.Spec.Name = "" }},
		{"clear logoURL", func(o *v1alpha1.DexClient) { o.Spec.LogoURL = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, dexClient("grafana", func(o *v1alpha1.DexClient) {
				o.Spec.AllowedConnectors = []string{"github"}
				o.Spec.TrustedPeers = []string{"kubectl"}
				o.Spec.LogoURL = "https://example.com/logo.svg"
			}))
			h.mustReconcile(t, "grafana")
			h.dex.ResetCalls()
			h.update(t, "grafana", tt.mutate)
			h.mustReconcile(t, "grafana")
			if h.dex.Calls("DeleteClient") != 1 || h.dex.Calls("CreateClient") != 1 {
				t.Errorf("DeleteClient=%d CreateClient=%d, want a recreate",
					h.dex.Calls("DeleteClient"), h.dex.Calls("CreateClient"))
			}
			want := desired(h.get(t, "grafana"), h.dex.Client("monitoring-grafana").Secret)
			if got := h.dex.Client("monitoring-grafana"); !equal(got, want) {
				t.Errorf("dex client = %v, want %v", got, want)
			}
		})
	}
}

func TestPublic(t *testing.T) {
	h := newHarness(t, dexClient("cli", func(o *v1alpha1.DexClient) {
		o.Spec.Public = true
		o.Spec.RedirectURIs = nil
		o.Spec.ClientID = "kubectl"
	}))
	h.mustReconcile(t, "cli")
	obj := h.get(t, "cli")
	assertReady(t, obj, metav1.ConditionTrue, v1alpha1.ReasonSynced)
	if obj.Status.SecretName != "" || obj.Status.ClientID != "kubectl" {
		t.Errorf("status = %+v, want clientID kubectl and no secret", obj.Status)
	}
	dc := h.dex.Client("kubectl")
	if dc == nil || !dc.Public || dc.Secret != "" {
		t.Fatalf("dex client = %v, want a public client without a secret", dc)
	}
	var list corev1.SecretList
	if err := h.c.List(context.Background(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 0 {
		t.Errorf("Secrets created for a public client: %d", len(list.Items))
	}

	// Turning a public client confidential recreates it with a secret.
	h.update(t, "cli", func(o *v1alpha1.DexClient) {
		o.Spec.Public = false
		o.Spec.RedirectURIs = []string{"http://localhost:8000"}
	})
	h.mustReconcile(t, "cli")
	if dc := h.dex.Client("kubectl"); dc.Public || dc.Secret == "" {
		t.Errorf("dex client = %v, want confidential with a secret", dc)
	}
}

func TestConflict(t *testing.T) {
	older := dexClient("grafana", func(o *v1alpha1.DexClient) { o.Spec.ClientID = "grafana" })
	younger := dexClient("grafana-2", func(o *v1alpha1.DexClient) {
		o.Spec.ClientID = "grafana"
		o.Spec.Name = "Impostor"
		o.CreationTimestamp = metav1.NewTime(t0.Add(time.Minute))
	})
	h := newHarness(t, older, younger)

	h.mustReconcile(t, "grafana-2")
	assertReady(t, h.get(t, "grafana-2"), metav1.ConditionFalse, v1alpha1.ReasonConflict)
	if h.dex.Client("grafana") != nil {
		t.Fatal("the younger claimant created the client")
	}
	h.mustReconcile(t, "grafana")
	assertReady(t, h.get(t, "grafana"), metav1.ConditionTrue, v1alpha1.ReasonSynced)

	// sameID maps either claimant to the other.
	reqs := h.r.sameID(context.Background(), h.get(t, "grafana"))
	if len(reqs) != 1 || reqs[0].Name != "grafana-2" {
		t.Errorf("sameID = %v, want grafana-2", reqs)
	}

	// The younger claimant's deletion leaves the winner's client alone.
	h.mustReconcile(t, "grafana-2") // adds its finalizer
	if err := h.c.Delete(context.Background(), h.get(t, "grafana-2")); err != nil {
		t.Fatal(err)
	}
	h.dex.ResetCalls()
	if _, err := h.reconcile(t, "grafana-2"); err != nil {
		t.Fatal(err)
	}
	if h.dex.Calls("DeleteClient") != 0 || h.dex.Client("grafana") == nil {
		t.Fatal("deleting the conflicting claimant deleted the winner's dex client")
	}

	// Recreate the younger claimant, then delete the winner: the younger
	// takes the client over instead of it being deleted.
	younger = dexClient("grafana-3", func(o *v1alpha1.DexClient) {
		o.Spec.ClientID = "grafana"
		o.Spec.Name = "Successor"
		o.CreationTimestamp = metav1.NewTime(t0.Add(time.Hour))
	})
	if err := h.c.Create(context.Background(), younger); err != nil {
		t.Fatal(err)
	}
	if err := h.c.Delete(context.Background(), h.get(t, "grafana")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.reconcile(t, "grafana"); err != nil {
		t.Fatal(err)
	}
	if h.dex.Calls("DeleteClient") != 0 {
		t.Fatal("the outgoing winner deleted a client a successor claims")
	}
	h.mustReconcile(t, "grafana-3")
	assertReady(t, h.get(t, "grafana-3"), metav1.ConditionTrue, v1alpha1.ReasonSynced)
	if got := h.dex.Client("grafana").Name; got != "Successor" {
		t.Errorf("dex client name = %q, want the successor's", got)
	}
}

func TestReadOnly(t *testing.T) {
	h := newHarness(t, dexClient("grafana", func(o *v1alpha1.DexClient) { o.Spec.ClientID = "static-app" }))
	h.dex.AddStaticClient(&api.Client{Id: "static-app", Secret: "from-config", RedirectUris: []string{"https://x"}})
	h.mustReconcile(t, "grafana")
	assertReady(t, h.get(t, "grafana"), metav1.ConditionFalse, v1alpha1.ReasonReadOnly)

	// Deleting it must not try to (or be blocked trying to) delete the
	// static client.
	if err := h.c.Delete(context.Background(), h.get(t, "grafana")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.reconcile(t, "grafana"); err != nil {
		t.Fatalf("finalize a ReadOnly client: %v", err)
	}
	var obj v1alpha1.DexClient
	err := h.c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "grafana"}, &obj)
	if !apierrors.IsNotFound(err) {
		t.Errorf("object still present after finalize (err %v)", err)
	}
}

func TestDelete(t *testing.T) {
	h := newHarness(t, dexClient("grafana"))
	h.mustReconcile(t, "grafana")
	if err := h.c.Delete(context.Background(), h.get(t, "grafana")); err != nil {
		t.Fatal(err)
	}

	// dex down: the finalizer holds.
	h.dex.Err = dextest.ErrUnavailable
	if _, err := h.reconcile(t, "grafana"); err == nil {
		t.Fatal("finalize with dex unavailable returned nil")
	}
	if !controllerutil.ContainsFinalizer(h.get(t, "grafana"), v1alpha1.Finalizer) {
		t.Fatal("finalizer removed although dex was unreachable")
	}

	h.dex.Err = nil
	if _, err := h.reconcile(t, "grafana"); err != nil {
		t.Fatal(err)
	}
	if h.dex.Client("monitoring-grafana") != nil {
		t.Error("dex client survived the DexClient's deletion")
	}
	var obj v1alpha1.DexClient
	err := h.c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "grafana"}, &obj)
	if !apierrors.IsNotFound(err) {
		t.Errorf("object still present after finalize (err %v)", err)
	}

	// Gone already: nothing to do.
	if _, err := h.reconcile(t, "grafana"); err != nil {
		t.Errorf("reconcile of a deleted object: %v", err)
	}
}

func TestDriftRepair(t *testing.T) {
	h := newHarness(t, dexClient("grafana"))
	h.mustReconcile(t, "grafana")
	h.dex.Wipe() // dex on memory storage restarted
	h.mustReconcile(t, "grafana")
	dc := h.dex.Client("monitoring-grafana")
	if dc == nil {
		t.Fatal("client not recreated after dex lost it")
	}
	if want := string(h.secret(t, "grafana-dex-client").Data["clientSecret"]); dc.Secret != want {
		t.Error("recreated client does not carry the generated secret")
	}
}

func TestDexUnavailable(t *testing.T) {
	h := newHarness(t, dexClient("grafana"))
	h.dex.Err = dextest.ErrUnavailable
	_, err := h.reconcile(t, "grafana")
	var ce *controller.ConditionError
	if !errors.As(err, &ce) || !ce.Transient {
		t.Fatalf("Reconcile error = %v, want a transient ConditionError for a backoff retry", err)
	}
	assertReady(t, h.get(t, "grafana"), metav1.ConditionFalse, v1alpha1.ReasonDexUnavailable)
}

func TestDexRejected(t *testing.T) {
	h := newHarness(t, dexClient("grafana", func(o *v1alpha1.DexClient) { o.Spec.RefreshTokenLifetime = "forever" }))
	h.mustReconcile(t, "grafana")
	assertReady(t, h.get(t, "grafana"), metav1.ConditionFalse, v1alpha1.ReasonDexRejected)
}

func TestForSecret(t *testing.T) {
	h := newHarness(t,
		dexClient("a", withSecretRef("shared", "clientSecret")),
		dexClient("b", withSecretRef("shared", "other")),
		dexClient("c"),
	)
	got := h.r.forSecret(context.Background(), secret("shared", nil))
	names := make([]string, 0, len(got))
	for _, r := range got {
		names = append(names, r.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"a", "b"}) {
		t.Errorf("forSecret(shared) = %v, want [a b]", names)
	}
	if got := h.r.forSecret(context.Background(), secret("c-dex-client", nil)); len(got) != 1 || got[0].Name != "c" {
		t.Errorf("forSecret(c-dex-client) = %v, want [c]", got)
	}
}
