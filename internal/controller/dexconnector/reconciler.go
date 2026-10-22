// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package dexconnector reconciles DexConnector objects into dex connectors
// through dex's gRPC API. dex must run with DEX_API_CONNECTORS_CRUD=true;
// it reloads a connector lazily when its resource version changes, so a
// change never restarts dex.
package dexconnector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	api "github.com/dexidp/dex/api/v2"
	corev1 "k8s.io/api/core/v1"
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

// Reconciler reconciles DexConnector objects.
type Reconciler struct {
	// Client reads DexConnectors from the cache and writes them.
	Client client.Client
	// APIReader reads Secrets uncached.
	APIReader client.Reader
	// Dex is dex's gRPC API.
	Dex dex.Client
	// ResyncPeriod re-reconciles every object this often to repair drift
	// in dex.
	ResyncPeriod time.Duration
	// Recorder emits events; nil disables them.
	Recorder events.EventRecorder
	// Log is the reconciler's logger.
	Log *slog.Logger
}

// Reconcile implements reconcile.Reconciler.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var obj v1alpha1.DexConnector
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
		fmt.Sprintf("connector %q is registered in dex", obj.DexID())) {
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
func (r *Reconciler) sync(ctx context.Context, obj *v1alpha1.DexConnector) error {
	id := obj.DexID()
	obj.Status.ID = id

	owner, err := r.claimant(ctx, obj)
	if err != nil {
		return err
	}
	if owner != nil && !controller.SameObject(owner, obj) {
		return controller.Conditionf(v1alpha1.ReasonConflict,
			"connector ID %q is already claimed by DexConnector %s/%s", id, owner.GetNamespace(), owner.GetName())
	}

	cfg, err := r.config(ctx, obj)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()
	return r.apply(ctx, obj, &api.Connector{
		Id:         id,
		Type:       obj.Spec.Type,
		Name:       obj.DisplayName(),
		Config:     cfg,
		GrantTypes: obj.Spec.GrantTypes,
	})
}

// claimant returns the DexConnector that owns obj's dex ID, or nil when
// every claimant (obj included) is being deleted.
func (r *Reconciler) claimant(ctx context.Context, obj *v1alpha1.DexConnector) (client.Object, error) {
	var list v1alpha1.DexConnectorList
	if err := r.Client.List(ctx, &list, client.MatchingFields{controller.IndexDexID: obj.DexID()}); err != nil {
		return nil, fmt.Errorf("list DexConnectors by ID: %w", err)
	}
	claims := []*v1alpha1.DexConnector{obj}
	for i := range list.Items {
		if !controller.SameObject(&list.Items[i], obj) {
			claims = append(claims, &list.Items[i])
		}
	}
	owner, ok := controller.Claimant(claims)
	if !ok {
		return nil, nil
	}
	return owner, nil
}

// config builds the connector config: spec.config with every secretRef's
// value merged in, canonicalized.
func (r *Reconciler) config(ctx context.Context, obj *v1alpha1.DexConnector) ([]byte, error) {
	values := make([]FieldValue, 0, len(obj.Spec.SecretRefs))
	for _, ref := range obj.Spec.SecretRefs {
		v, err := controller.SecretValue(ctx, r.APIReader, obj.Namespace, ref.ValueFrom.Name, ref.ValueFrom.Key)
		if err != nil {
			return nil, err
		}
		values = append(values, FieldValue{Path: ref.ConfigField, Value: v})
	}
	var raw []byte
	if obj.Spec.Config != nil {
		raw = obj.Spec.Config.Raw
	}
	cfg, err := BuildConfig(raw, values)
	if err != nil {
		return nil, controller.Conditionf(v1alpha1.ReasonInvalidConfig, "%v", err)
	}
	return cfg, nil
}

// apply makes dex's connector match want. dex has no GetConnector, so the
// connector is found in ListConnectors. It is updated only on a real
// difference: every update bumps the connector's resource version, which
// makes dex rebuild it.
func (r *Reconciler) apply(ctx context.Context, obj *v1alpha1.DexConnector, want *api.Connector) error {
	list, err := r.Dex.ListConnectors(ctx, &api.ListConnectorReq{})
	if err != nil {
		return controller.DexError(err)
	}
	var got *api.Connector
	for _, c := range list.GetConnectors() {
		if c.GetId() == want.Id {
			got = c
			break
		}
	}
	if got == nil {
		return r.create(ctx, obj, want)
	}
	if same(got, want) {
		return nil
	}
	upd, err := r.Dex.UpdateConnector(ctx, &api.UpdateConnectorReq{
		Id:            want.Id,
		NewType:       want.Type,
		NewName:       want.Name,
		NewConfig:     want.Config,
		NewGrantTypes: &api.GrantTypes{GrantTypes: want.GrantTypes},
	})
	if err != nil {
		return controller.DexError(err)
	}
	if upd.NotFound {
		return r.create(ctx, obj, want)
	}
	r.Log.LogAttrs(ctx, slog.LevelInfo, "updated dex connector", slog.String("connector_id", want.Id))
	controller.Event(r.Recorder, obj, corev1.EventTypeNormal, "Updated", "Update",
		"updated connector %q in dex", want.Id)
	return nil
}

func (r *Reconciler) create(ctx context.Context, obj *v1alpha1.DexConnector, want *api.Connector) error {
	resp, err := r.Dex.CreateConnector(ctx, &api.CreateConnectorReq{Connector: want})
	if err != nil {
		return controller.DexError(err)
	}
	if resp.AlreadyExists {
		return &controller.ConditionError{
			Reason:    v1alpha1.ReasonDexRejected,
			Message:   fmt.Sprintf("connector %q appeared in dex concurrently; retrying", want.Id),
			Transient: true,
		}
	}
	r.Log.LogAttrs(ctx, slog.LevelInfo, "created dex connector", slog.String("connector_id", want.Id))
	controller.Event(r.Recorder, obj, corev1.EventTypeNormal, "Created", "Create",
		"created connector %q in dex", want.Id)
	return nil
}

// same reports whether dex's connector got already matches want. Configs
// compare in canonical form; an unparsable config in dex counts as
// different.
func same(got, want *api.Connector) bool {
	if got.Type != want.Type || got.Name != want.Name || !slices.Equal(got.GrantTypes, want.GrantTypes) {
		return false
	}
	norm, err := Normalize(got.Config)
	return err == nil && bytes.Equal(norm, want.Config)
}

// finalize deletes obj's dex connector — unless another DexConnector still
// claims the ID, which then takes the connector over — and releases obj.
func (r *Reconciler) finalize(ctx context.Context, obj *v1alpha1.DexConnector) error {
	if !controllerutil.ContainsFinalizer(obj, v1alpha1.Finalizer) {
		return nil
	}
	owner, err := r.claimant(ctx, obj)
	if err != nil {
		return err
	}
	if owner == nil {
		ctx, cancel := context.WithTimeout(ctx, rpcTimeout)
		defer cancel()
		_, err := r.Dex.DeleteConnector(ctx, &api.DeleteConnectorReq{Id: obj.DexID()})
		switch {
		case dex.IsReadOnly(err):
			// A static connector: never ours to delete.
		case err != nil:
			// The finalizer holds until dex confirms: a connector left
			// behind would keep accepting logins. A FeatureDisabled dex
			// blocks here until the flag is back on (or the finalizer is
			// removed by hand).
			before := obj.DeepCopy()
			controller.SetReady(&obj.Status.Conditions, obj.Generation, controller.DexError(err), "")
			if perr := controller.PatchStatus(ctx, r.Client, obj, before, obj.Status, before.Status); perr != nil {
				return perr
			}
			return fmt.Errorf("delete dex connector %q: %w", obj.DexID(), err)
		default:
			r.Log.LogAttrs(ctx, slog.LevelInfo, "deleted dex connector", slog.String("connector_id", obj.DexID()))
		}
	}
	return controller.RemoveFinalizer(ctx, r.Client, obj)
}

// IndexDexID is the IndexDexID extractor.
func IndexDexID(o client.Object) []string { return []string{o.(*v1alpha1.DexConnector).DexID()} }

// IndexSecretName is the IndexSecretName extractor.
func IndexSecretName(o client.Object) []string {
	refs := o.(*v1alpha1.DexConnector).Spec.SecretRefs
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		if !slices.Contains(names, ref.ValueFrom.Name) {
			names = append(names, ref.ValueFrom.Name)
		}
	}
	return names
}

// SetupIndexes registers the field indexes the reconciler queries.
func SetupIndexes(ctx context.Context, idx client.FieldIndexer) error {
	if err := idx.IndexField(ctx, &v1alpha1.DexConnector{}, controller.IndexDexID, IndexDexID); err != nil {
		return err
	}
	return idx.IndexField(ctx, &v1alpha1.DexConnector{}, controller.IndexSecretName, IndexSecretName)
}

// SetupWithManager registers the reconciler and its watches: DexConnectors,
// other DexConnectors sharing an ID, and Secrets metadata-only.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if err := SetupIndexes(ctx, mgr.GetFieldIndexer()); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named("dexconnector").
		For(&v1alpha1.DexConnector{}).
		Watches(&v1alpha1.DexConnector{}, handler.EnqueueRequestsFromMapFunc(r.sameID)).
		WatchesMetadata(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.forSecret)).
		Complete(r)
}

// sameID maps a DexConnector to every other DexConnector claiming its ID.
func (r *Reconciler) sameID(ctx context.Context, o client.Object) []reconcile.Request {
	var list v1alpha1.DexConnectorList
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

// forSecret maps a Secret to the DexConnectors in its namespace that read
// it.
func (r *Reconciler) forSecret(ctx context.Context, o client.Object) []reconcile.Request {
	var list v1alpha1.DexConnectorList
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
