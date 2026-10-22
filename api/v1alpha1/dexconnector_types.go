// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DexConnectorSpec describes one upstream identity provider dex federates to.
//
// +kubebuilder:validation:XValidation:rule="has(self.id) == has(oldSelf.id) && (!has(self.id) || self.id == oldSelf.id)",message="id is immutable"
type DexConnectorSpec struct {
	// ID is the connector ID registered in dex (what DexClient
	// allowedConnectors and dex's login URLs refer to). Defaults to
	// "<namespace>-<name>"; immutable once the object exists.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	ID string `json:"id,omitempty"`

	// Type is the dex connector type: oidc, github, ldap, saml, microsoft,
	// ... — any type the running dex was built with.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Type string `json:"type"`

	// Name is the human-readable name dex shows on its login page. Defaults
	// to the object's name.
	// +optional
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`

	// Config is the connector type's own configuration, exactly as it would
	// appear under `config:` in dex's config file. Secret values do not
	// belong here; reference them with SecretRefs.
	// +optional
	// +kubebuilder:validation:Type=object
	// +kubebuilder:pruning:PreserveUnknownFields
	Config *apiextensionsv1.JSON `json:"config,omitempty"`

	// SecretRefs merge Secret values into Config before it is sent to dex.
	// +optional
	// +listType=map
	// +listMapKey=configField
	// +kubebuilder:validation:MaxItems=32
	SecretRefs []ConnectorSecretRef `json:"secretRefs,omitempty"`

	// GrantTypes restricts the OAuth2 grant types this connector may be
	// used with. Empty leaves the connector unrestricted.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:validation:items:Enum=authorization_code;refresh_token;implicit;password;"urn:ietf:params:oauth:grant-type:device_code";"urn:ietf:params:oauth:grant-type:token-exchange"
	GrantTypes []string `json:"grantTypes,omitempty"`
}

// ConnectorSecretRef sets one config field from a Secret value.
type ConnectorSecretRef struct {
	// ConfigField is the dotted path in Config the value is written to:
	// "clientSecret", "bindPW", "nested.key". Intermediate objects are
	// created as needed; a path through a non-object value is an error.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$`
	ConfigField string `json:"configField"`
	// ValueFrom is the Secret key, in the DexConnector's namespace, holding
	// the value.
	ValueFrom SecretKeySelector `json:"valueFrom"`
}

// DexConnectorStatus is the observed state of a DexConnector.
type DexConnectorStatus struct {
	// ID is the connector ID registered in dex.
	// +optional
	ID string `json:"id,omitempty"`
	// ObservedGeneration is the generation the conditions describe.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions carry the Ready summary.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// DexConnector registers one upstream identity provider in dex. dex must run
// with DEX_API_CONNECTORS_CRUD=true; connectors affect every login, so
// creating them is usually reserved to platform administrators by RBAC.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories=dex
// +kubebuilder:printcolumn:name="ID",type=string,JSONPath=`.status.id`
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type DexConnector struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DexConnectorSpec   `json:"spec"`
	Status DexConnectorStatus `json:"status,omitempty"`
}

// DexConnectorList is a list of DexConnectors.
//
// +kubebuilder:object:root=true
type DexConnectorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DexConnector `json:"items"`
}

// DexID is the connector ID this object registers in dex: spec.id, or
// "<namespace>-<name>" when unset.
func (c *DexConnector) DexID() string {
	if c.Spec.ID != "" {
		return c.Spec.ID
	}
	return defaultID(c.Namespace, c.Name)
}

// DisplayName is the connector name sent to dex: spec.name, or the object's
// name when unset (dex requires one).
func (c *DexConnector) DisplayName() string {
	if c.Spec.Name != "" {
		return c.Spec.Name
	}
	return c.Name
}
