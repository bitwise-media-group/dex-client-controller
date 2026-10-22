// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// +kubebuilder:object:generate=true
// +groupName=dex.bitwisemedia.uk

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// The api package deliberately depends only on apimachinery (no
// controller-runtime): it is imported by the e2e module and by anything that
// wants to build these objects without pulling in a controller.
var (
	// GroupVersion is the group and version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "dex.bitwisemedia.uk", Version: "v1alpha1"}

	// SchemeBuilder collects the functions that register this group-version's
	// types into a scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

// addKnownTypes registers every kind in the group with the scheme.
func addKnownTypes(s *runtime.Scheme) error {
	s.AddKnownTypes(GroupVersion,
		&DexClient{}, &DexClientList{},
		&DexConnector{}, &DexConnectorList{},
	)
	metav1.AddToGroupVersion(s, GroupVersion)
	return nil
}
