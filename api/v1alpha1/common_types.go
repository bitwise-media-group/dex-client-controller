// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package v1alpha1

// SecretKeySelector names one key of a Secret in the object's own namespace.
type SecretKeySelector struct {
	// Name of the Secret.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`
	// Key within the Secret's data.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Key string `json:"key"`
}

// defaultID is the dex ID an object gets when its spec names none.
func defaultID(namespace, name string) string { return namespace + "-" + name }
