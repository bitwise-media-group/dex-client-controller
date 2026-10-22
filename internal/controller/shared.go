// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package controller holds what the DexClient and DexConnector reconcilers
// share: the field indexes, ID-conflict resolution, Secret reads, the
// condition plumbing, and the status/finalizer patches. The reconcilers
// themselves live in the dexclient and dexconnector subpackages.
package controller

import (
	"context"
	"errors"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	v1alpha1 "github.com/bitwise-media-group/dex-client-controller/api/v1alpha1"
	"github.com/bitwise-media-group/dex-client-controller/internal/dex"
)

// Field indexes registered on both kinds.
const (
	// IndexDexID indexes an object by the dex ID it resolves to, so a
	// reconcile can find every other object claiming the same ID.
	IndexDexID = "dex.bitwisemedia.uk/dexID"
	// IndexSecretName indexes an object by every Secret name it reads, so a
	// Secret change can be mapped back to the objects to reconcile.
	IndexSecretName = "dex.bitwisemedia.uk/secretName"
)

// FieldManager names the controller in managedFields and events.
const FieldManager = "dex-client-controller"

// ConditionError is a failure the reconciler reports on the Ready condition
// instead of (or as well as) returning it. Transient ones are also returned
// to controller-runtime for a rate-limited retry; the rest wait for the next
// change or resync.
type ConditionError struct {
	Reason    string
	Message   string
	Transient bool
}

func (e *ConditionError) Error() string { return e.Reason + ": " + e.Message }

// Conditionf builds a non-transient ConditionError.
func Conditionf(reason, format string, args ...any) *ConditionError {
	return &ConditionError{Reason: reason, Message: fmt.Sprintf(format, args...)}
}

// DexError wraps an error from a dex RPC as a ConditionError; an unreachable
// dex is transient.
func DexError(err error) *ConditionError {
	reason, msg := dex.Reason(err)
	return &ConditionError{Reason: reason, Message: msg, Transient: dex.IsUnavailable(err)}
}

// Claimant is the object that owns a dex ID among every object claiming it:
// the oldest by creationTimestamp, ties broken by namespace/name. Objects
// being deleted have given their claim up, so a younger claimant takes over
// the dex entry instead of the deleting one removing it.
func Claimant[T client.Object](objs []T) (T, bool) {
	var zero T
	live := make([]T, 0, len(objs))
	for _, o := range objs {
		if o.GetDeletionTimestamp().IsZero() {
			live = append(live, o)
		}
	}
	if len(live) == 0 {
		return zero, false
	}
	sort.Slice(live, func(i, j int) bool {
		a, b := live[i], live[j]
		at, bt := a.GetCreationTimestamp(), b.GetCreationTimestamp()
		if !at.Equal(&bt) {
			return at.Before(&bt)
		}
		if a.GetNamespace() != b.GetNamespace() {
			return a.GetNamespace() < b.GetNamespace()
		}
		return a.GetName() < b.GetName()
	})
	return live[0], true
}

// SameObject reports whether a and b are the same object.
func SameObject(a, b client.Object) bool {
	return a.GetNamespace() == b.GetNamespace() && a.GetName() == b.GetName()
}

// SecretValue reads one key of a Secret through r (the uncached API reader:
// Secrets are never cached). A missing Secret or key, or an empty value, is
// a SecretNotFound ConditionError.
func SecretValue(ctx context.Context, r client.Reader, namespace, name, key string) (string, error) {
	var s corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &s); err != nil {
		if apierrors.IsNotFound(err) {
			return "", Conditionf(v1alpha1.ReasonSecretNotFound, "Secret %s/%s not found", namespace, name)
		}
		return "", fmt.Errorf("get Secret %s/%s: %w", namespace, name, err)
	}
	v := s.Data[key]
	if len(v) == 0 {
		return "", Conditionf(v1alpha1.ReasonSecretNotFound, "Secret %s/%s has no (or an empty) key %q",
			namespace, name, key)
	}
	return string(v), nil
}

// SetReady records the Ready condition for the outcome of a sync: nil is
// Synced, a ConditionError its reason. It reports false for any other error,
// which leaves the conditions untouched (a Kubernetes API failure says
// nothing about the dex state).
func SetReady(conds *[]metav1.Condition, generation int64, err error, syncedMsg string) bool {
	cond := metav1.Condition{
		Type:               v1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             v1alpha1.ReasonSynced,
		Message:            syncedMsg,
		ObservedGeneration: generation,
	}
	if err != nil {
		var ce *ConditionError
		if !errors.As(err, &ce) {
			return false
		}
		cond.Status, cond.Reason, cond.Message = metav1.ConditionFalse, ce.Reason, ce.Message
	}
	meta.SetStatusCondition(conds, cond)
	return true
}

// PatchStatus writes obj's status if it differs from before's.
func PatchStatus(ctx context.Context, c client.Client, obj, before client.Object, status, beforeStatus any) error {
	if equality.Semantic.DeepEqual(status, beforeStatus) {
		return nil
	}
	if err := c.Status().Patch(ctx, obj, client.MergeFrom(before), client.FieldOwner(FieldManager)); err != nil {
		return client.IgnoreNotFound(err)
	}
	return nil
}

// Event emits a Kubernetes event when a recorder is configured.
func Event(rec events.EventRecorder, obj client.Object, eventType, reason, action, note string, args ...any) {
	if rec != nil {
		rec.Eventf(obj, nil, eventType, reason, action, note, args...)
	}
}

// AddFinalizer adds the dex finalizer to obj if it is missing. The patch
// carries an optimistic lock: a merge patch replaces the whole finalizers
// list, so a concurrent writer's finalizer must not be lost.
func AddFinalizer(ctx context.Context, c client.Client, obj client.Object) error {
	if controllerutil.ContainsFinalizer(obj, v1alpha1.Finalizer) {
		return nil
	}
	before := obj.DeepCopyObject().(client.Object)
	controllerutil.AddFinalizer(obj, v1alpha1.Finalizer)
	return c.Patch(ctx, obj, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}),
		client.FieldOwner(FieldManager))
}

// RemoveFinalizer removes the dex finalizer from obj, releasing it for
// deletion.
func RemoveFinalizer(ctx context.Context, c client.Client, obj client.Object) error {
	if !controllerutil.ContainsFinalizer(obj, v1alpha1.Finalizer) {
		return nil
	}
	before := obj.DeepCopyObject().(client.Object)
	controllerutil.RemoveFinalizer(obj, v1alpha1.Finalizer)
	err := c.Patch(ctx, obj, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}),
		client.FieldOwner(FieldManager))
	return client.IgnoreNotFound(err)
}
