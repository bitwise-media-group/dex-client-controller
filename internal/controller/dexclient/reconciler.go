// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package dexclient reconciles DexClient objects into dex OAuth2 clients
// through dex's gRPC API.
package dexclient

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	api "github.com/dexidp/dex/api/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/bitwise-media-group/dex-client-controller/api/v1alpha1"
	"github.com/bitwise-media-group/dex-client-controller/internal/controller"
	"github.com/bitwise-media-group/dex-client-controller/internal/dex"
)

// rpcTimeout bounds the dex calls one reconcile makes.
const rpcTimeout = 30 * time.Second

// secretBytes is the entropy of a generated client secret.
const secretBytes = 32

// Reconciler reconciles DexClient objects.
type Reconciler struct {
	// Client reads DexClients from the cache and writes everything.
	Client client.Client
	// APIReader reads Secrets uncached.
	APIReader client.Reader
	// Scheme resolves the owner reference on generated Secrets.
	Scheme *runtime.Scheme
	// Dex is dex's gRPC API.
	Dex dex.Client
	// Issuer, when set, is written into generated Secrets so an app can
	// read its whole OIDC configuration from one place.
	Issuer string
	// ResyncPeriod re-reconciles every object this often to repair drift
	// in dex (dex has no change events; memory storage forgets on restart).
	ResyncPeriod time.Duration
	// Recorder emits events; nil disables them.
	Recorder events.EventRecorder
	// Log is the reconciler's logger.
	Log *slog.Logger
}

// Reconcile implements reconcile.Reconciler.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var obj v1alpha1.DexClient
	if err := r.Client.Get(ctx, req.NamespacedName, &obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !obj.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalize(ctx, &obj)
	}
	if err := controller.AddFinalizer(ctx, r.Client, &obj); err != nil {
		return ctrl.Result{}, err
	}

	before := obj.DeepCopy()
	err := r.sync(ctx, &obj)
	obj.Status.ObservedGeneration = obj.Generation
	if !controller.SetReady(&obj.Status.Conditions, obj.Generation, err,
		fmt.Sprintf("client %q is registered in dex", obj.DexID())) {
		return ctrl.Result{}, err
	}
	if perr := controller.PatchStatus(ctx, r.Client, &obj, before, obj.Status, before.Status); perr != nil {
		return ctrl.Result{}, perr
	}
	var ce *controller.ConditionError
	if errors.As(err, &ce) && ce.Transient {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: r.ResyncPeriod}, nil
}

// sync drives dex towards obj's spec and fills obj's status fields; the
// returned error decides the Ready condition.
func (r *Reconciler) sync(ctx context.Context, obj *v1alpha1.DexClient) error {
	id := obj.DexID()
	obj.Status.ClientID = id
	obj.Status.SecretName = obj.SecretName()

	owner, err := r.claimant(ctx, obj)
	if err != nil {
		return err
	}
	if !controller.SameObject(owner, obj) {
		return controller.Conditionf(v1alpha1.ReasonConflict,
			"client ID %q is already claimed by DexClient %s/%s", id, owner.GetNamespace(), owner.GetName())
	}

	secret, err := r.secret(ctx, obj)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()
	return r.apply(ctx, obj, desired(obj, secret))
}

// claimant returns the DexClient that owns obj's dex ID.
func (r *Reconciler) claimant(ctx context.Context, obj *v1alpha1.DexClient) (client.Object, error) {
	var list v1alpha1.DexClientList
	if err := r.Client.List(ctx, &list, client.MatchingFields{controller.IndexDexID: obj.DexID()}); err != nil {
		return nil, fmt.Errorf("list DexClients by ID: %w", err)
	}
	claims := []*v1alpha1.DexClient{obj}
	for i := range list.Items {
		if !controller.SameObject(&list.Items[i], obj) {
			claims = append(claims, &list.Items[i])
		}
	}
	owner, ok := controller.Claimant(claims)
	if !ok {
		return nil, nil // every claimant, obj included, is being deleted
	}
	return owner, nil
}

// secret resolves the client secret: none for a public client, the
// referenced Secret's value, or the generated Secret's (created on first
// use).
func (r *Reconciler) secret(ctx context.Context, obj *v1alpha1.DexClient) (string, error) {
	switch {
	case obj.Spec.Public:
		return "", nil
	case obj.Spec.SecretRef != nil:
		return controller.SecretValue(ctx, r.APIReader, obj.Namespace, obj.Spec.SecretRef.Name, obj.SecretKey())
	default:
		return r.generatedSecret(ctx, obj)
	}
}

// generatedSecret returns the client secret from the Secret the controller
// owns for obj, creating the Secret (or refilling its keys) as needed.
func (r *Reconciler) generatedSecret(ctx context.Context, obj *v1alpha1.DexClient) (string, error) {
	name := obj.GeneratedSecretName()
	var s corev1.Secret
	err := r.APIReader.Get(ctx, types.NamespacedName{Namespace: obj.Namespace, Name: name}, &s)
	switch {
	case apierrors.IsNotFound(err):
		value, err := randomSecret()
		if err != nil {
			return "", err
		}
		s = corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: obj.Namespace,
				Labels:    map[string]string{"app.kubernetes.io/managed-by": controller.FieldManager},
			},
			Type: corev1.SecretTypeOpaque,
			Data: r.secretData(obj, value),
		}
		if err := controllerutil.SetControllerReference(obj, &s, r.Scheme); err != nil {
			return "", err
		}
		if err := r.Client.Create(ctx, &s, client.FieldOwner(controller.FieldManager)); err != nil {
			return "", fmt.Errorf("create Secret %s/%s: %w", obj.Namespace, name, err)
		}
		controller.Event(r.Recorder, obj, corev1.EventTypeNormal, "SecretGenerated", "Create",
			"generated client secret in Secret %s", name)
		return value, nil
	case err != nil:
		return "", fmt.Errorf("get Secret %s/%s: %w", obj.Namespace, name, err)
	}

	if !metav1.IsControlledBy(&s, obj) {
		return "", controller.Conditionf(v1alpha1.ReasonInvalidConfig,
			"Secret %s already exists and is not owned by this DexClient; "+
				"set spec.generatedSecret.name, or point spec.secretRef at it", name)
	}
	value := string(s.Data[v1alpha1.GeneratedSecretKeyClientSecret])
	if value == "" {
		if value, err = randomSecret(); err != nil {
			return "", err
		}
	}
	want := r.secretData(obj, value)
	merged := maps.Clone(s.Data)
	if merged == nil {
		merged = map[string][]byte{}
	}
	maps.Copy(merged, want)
	if r.Issuer == "" {
		delete(merged, v1alpha1.GeneratedSecretKeyIssuer)
	}
	if !maps.EqualFunc(merged, s.Data, slices.Equal) {
		s.Data = merged
		if err := r.Client.Update(ctx, &s, client.FieldOwner(controller.FieldManager)); err != nil {
			return "", fmt.Errorf("update Secret %s/%s: %w", obj.Namespace, name, err)
		}
	}
	return value, nil
}

func (r *Reconciler) secretData(obj *v1alpha1.DexClient, value string) map[string][]byte {
	data := map[string][]byte{
		v1alpha1.GeneratedSecretKeyClientID:     []byte(obj.DexID()),
		v1alpha1.GeneratedSecretKeyClientSecret: []byte(value),
	}
	if r.Issuer != "" {
		data[v1alpha1.GeneratedSecretKeyIssuer] = []byte(r.Issuer)
	}
	return data
}

func randomSecret() (string, error) {
	b := make([]byte, secretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate client secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// desired is the dex client obj describes.
func desired(obj *v1alpha1.DexClient, secret string) *api.Client {
	s := obj.Spec
	return &api.Client{
		Id:                     obj.DexID(),
		Secret:                 secret,
		Public:                 s.Public,
		Name:                   s.Name,
		LogoUrl:                s.LogoURL,
		RedirectUris:           s.RedirectURIs,
		PostLogoutRedirectUris: s.PostLogoutRedirectURIs,
		TrustedPeers:           s.TrustedPeers,
		AllowedConnectors:      s.AllowedConnectors,
		BackchannelLogoutUri:   s.BackchannelLogoutURI,
		RefreshTokenLifetime:   s.RefreshTokenLifetime,
	}
}

// apply makes dex's client match want.
func (r *Reconciler) apply(ctx context.Context, obj *v1alpha1.DexClient, want *api.Client) error {
	resp, err := r.Dex.GetClient(ctx, &api.GetClientReq{Id: want.Id})
	switch {
	case dex.IsNotFound(err):
		return r.create(ctx, obj, want, "Created", "created client %q in dex")
	case err != nil:
		return controller.DexError(err)
	}
	got := resp.GetClient()

	if why := recreateReason(got, want); why != "" {
		// UpdateClient cannot change these, so the client is replaced.
		if _, err := r.Dex.DeleteClient(ctx, &api.DeleteClientReq{Id: want.Id}); err != nil {
			return controller.DexError(err)
		}
		r.Log.LogAttrs(ctx, slog.LevelInfo, "recreating dex client",
			slog.String("client_id", want.Id), slog.String("reason", why))
		return r.create(ctx, obj, want, "Recreated", "recreated client %q in dex ("+why+")")
	}
	if equal(got, want) {
		return nil
	}
	upd, err := r.Dex.UpdateClient(ctx, &api.UpdateClientReq{
		Id:                     want.Id,
		Name:                   want.Name,
		LogoUrl:                want.LogoUrl,
		RedirectUris:           want.RedirectUris,
		PostLogoutRedirectUris: want.PostLogoutRedirectUris,
		TrustedPeers:           want.TrustedPeers,
		AllowedConnectors:      want.AllowedConnectors,
		// Sent explicitly so that an empty value clears them.
		BackchannelLogoutUri: &want.BackchannelLogoutUri,
		RefreshTokenLifetime: &want.RefreshTokenLifetime,
	})
	if err != nil {
		return controller.DexError(err)
	}
	if upd.NotFound {
		return r.create(ctx, obj, want, "Created", "created client %q in dex")
	}
	r.Log.LogAttrs(ctx, slog.LevelInfo, "updated dex client", slog.String("client_id", want.Id))
	controller.Event(r.Recorder, obj, corev1.EventTypeNormal, "Updated", "Update", "updated client %q in dex", want.Id)
	return nil
}

func (r *Reconciler) create(ctx context.Context, obj *v1alpha1.DexClient, want *api.Client, reason, note string) error {
	resp, err := r.Dex.CreateClient(ctx, &api.CreateClientReq{Client: want})
	if err != nil {
		return controller.DexError(err)
	}
	if resp.AlreadyExists {
		return &controller.ConditionError{
			Reason:    v1alpha1.ReasonDexRejected,
			Message:   fmt.Sprintf("client %q appeared in dex concurrently; retrying", want.Id),
			Transient: true,
		}
	}
	r.Log.LogAttrs(ctx, slog.LevelInfo, "created dex client", slog.String("client_id", want.Id))
	controller.Event(r.Recorder, obj, corev1.EventTypeNormal, reason, "Create", note, want.Id)
	return nil
}

// recreateReason names why dex's client got cannot be updated in place to
// want, or returns "". dex's UpdateClient never changes the secret or
// public flag, ignores an empty name or logo URL, and cannot clear a list
// (an empty repeated field does not survive the wire).
func recreateReason(got, want *api.Client) string {
	switch {
	case got.Secret != want.Secret:
		return "secret changed"
	case got.Public != want.Public:
		return "public changed"
	case want.Name == "" && got.Name != "":
		return "name cleared"
	case want.LogoUrl == "" && got.LogoUrl != "":
		return "logoURL cleared"
	case len(want.RedirectUris) == 0 && len(got.RedirectUris) > 0:
		return "redirectURIs cleared"
	case len(want.PostLogoutRedirectUris) == 0 && len(got.PostLogoutRedirectUris) > 0:
		return "postLogoutRedirectURIs cleared"
	case len(want.TrustedPeers) == 0 && len(got.TrustedPeers) > 0:
		return "trustedPeers cleared"
	case len(want.AllowedConnectors) == 0 && len(got.AllowedConnectors) > 0:
		return "allowedConnectors cleared"
	}
	return ""
}

// equal reports whether every field the controller manages matches.
func equal(got, want *api.Client) bool {
	return got.Name == want.Name &&
		got.LogoUrl == want.LogoUrl &&
		slices.Equal(got.RedirectUris, want.RedirectUris) &&
		slices.Equal(got.PostLogoutRedirectUris, want.PostLogoutRedirectUris) &&
		slices.Equal(got.TrustedPeers, want.TrustedPeers) &&
		slices.Equal(got.AllowedConnectors, want.AllowedConnectors) &&
		got.BackchannelLogoutUri == want.BackchannelLogoutUri &&
		got.RefreshTokenLifetime == want.RefreshTokenLifetime
}

// finalize deletes obj's dex client — unless another DexClient still claims
// the ID, which then takes the client over — and releases obj.
func (r *Reconciler) finalize(ctx context.Context, obj *v1alpha1.DexClient) error {
	if !controllerutil.ContainsFinalizer(obj, v1alpha1.Finalizer) {
		return nil
	}
	owner, err := r.claimant(ctx, obj)
	if err != nil {
		return err
	}
	// claimant skips objects being deleted, obj included: a non-nil owner
	// is another live DexClient with the same ID, which takes the client
	// over on its next reconcile.
	if owner == nil {
		ctx, cancel := context.WithTimeout(ctx, rpcTimeout)
		defer cancel()
		_, err := r.Dex.DeleteClient(ctx, &api.DeleteClientReq{Id: obj.DexID()})
		switch {
		case dex.IsReadOnly(err):
			// A static client: never ours to delete.
		case err != nil:
			r.Log.LogAttrs(ctx, slog.LevelWarn, "delete dex client",
				slog.String("client_id", obj.DexID()), slog.Any("error", err))
			return fmt.Errorf("delete dex client %q: %w", obj.DexID(), err)
		default:
			r.Log.LogAttrs(ctx, slog.LevelInfo, "deleted dex client", slog.String("client_id", obj.DexID()))
		}
	}
	return controller.RemoveFinalizer(ctx, r.Client, obj)
}

// IndexDexID is the IndexDexID extractor.
func IndexDexID(o client.Object) []string { return []string{o.(*v1alpha1.DexClient).DexID()} }

// IndexSecretName is the IndexSecretName extractor.
func IndexSecretName(o client.Object) []string {
	if name := o.(*v1alpha1.DexClient).SecretName(); name != "" {
		return []string{name}
	}
	return nil
}

// SetupIndexes registers the field indexes the reconciler queries.
func SetupIndexes(ctx context.Context, idx client.FieldIndexer) error {
	if err := idx.IndexField(ctx, &v1alpha1.DexClient{}, controller.IndexDexID, IndexDexID); err != nil {
		return err
	}
	return idx.IndexField(ctx, &v1alpha1.DexClient{}, controller.IndexSecretName, IndexSecretName)
}

// SetupWithManager registers the reconciler and its watches: DexClients,
// other DexClients sharing an ID (so a released ID is taken over at once),
// and Secrets metadata-only (their data never enters the cache).
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if err := SetupIndexes(ctx, mgr.GetFieldIndexer()); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named("dexclient").
		For(&v1alpha1.DexClient{}).
		Watches(&v1alpha1.DexClient{}, handler.EnqueueRequestsFromMapFunc(r.sameID)).
		WatchesMetadata(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.forSecret)).
		Complete(r)
}

// sameID maps a DexClient to every other DexClient claiming its dex ID.
func (r *Reconciler) sameID(ctx context.Context, o client.Object) []reconcile.Request {
	var list v1alpha1.DexClientList
	if err := r.Client.List(ctx, &list, client.MatchingFields{controller.IndexDexID: IndexDexID(o)[0]}); err != nil {
		return nil
	}
	var reqs []reconcile.Request
	for i := range list.Items {
		if !controller.SameObject(&list.Items[i], o) {
			reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
		}
	}
	return reqs
}

// forSecret maps a Secret to the DexClients in its namespace that read it.
func (r *Reconciler) forSecret(ctx context.Context, o client.Object) []reconcile.Request {
	var list v1alpha1.DexClientList
	if err := r.Client.List(ctx, &list, client.InNamespace(o.GetNamespace()),
		client.MatchingFields{controller.IndexSecretName: o.GetName()}); err != nil {
		return nil
	}
	reqs := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
	}
	return reqs
}
