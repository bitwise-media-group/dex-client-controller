// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package v1alpha1

// ConditionReady is the single summary condition both kinds carry: True once
// the object's state is in dex, False with one of the reasons below
// otherwise.
const ConditionReady = "Ready"

// Ready condition reasons.
const (
	// ReasonSynced: dex holds exactly what the spec describes.
	ReasonSynced = "Synced"
	// ReasonSecretNotFound: a referenced Secret, or a key in it, is missing
	// or empty. The controller retries when the Secret changes.
	ReasonSecretNotFound = "SecretNotFound"
	// ReasonInvalidConfig: the spec cannot be turned into a dex object (a
	// secretRef configField that collides with a non-object value, a
	// generated Secret name already taken by a Secret this object does not
	// own, ...).
	ReasonInvalidConfig = "InvalidConfig"
	// ReasonConflict: another object of the same kind claims the same dex
	// ID. The oldest claimant (by creationTimestamp) wins; the rest wait.
	ReasonConflict = "Conflict"
	// ReasonReadOnly: the dex ID belongs to a static client or connector in
	// dex's config file, which the API refuses to change.
	ReasonReadOnly = "ReadOnly"
	// ReasonFeatureDisabled: dex refuses connector CRUD because it runs
	// without DEX_API_CONNECTORS_CRUD=true.
	ReasonFeatureDisabled = "FeatureDisabled"
	// ReasonDexUnavailable: the dex gRPC API could not be reached.
	ReasonDexUnavailable = "DexUnavailable"
	// ReasonDexRejected: dex answered the request with an error.
	ReasonDexRejected = "DexRejected"
)

// Finalizer holds an object until its dex counterpart is deleted.
const Finalizer = "dex.bitwisemedia.uk/finalizer"
