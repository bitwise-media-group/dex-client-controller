// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Refresh token lifetimes dex accepts for a client.
const (
	// RefreshTokenLifetimeStandalone: refresh tokens outlive the browser
	// session that issued them (dex's default).
	RefreshTokenLifetimeStandalone = "standalone"
	// RefreshTokenLifetimeSession: refresh tokens end with the browser
	// session that issued them.
	RefreshTokenLifetimeSession = "session"
)

// Keys the controller writes into a generated client Secret.
const (
	GeneratedSecretKeyClientID     = "clientID"
	GeneratedSecretKeyClientSecret = "clientSecret"
	GeneratedSecretKeyIssuer       = "issuer"
)

// DexClientSpec describes one OAuth2 client registered in dex.
//
// +kubebuilder:validation:XValidation:rule="has(self.clientID) == has(oldSelf.clientID) && (!has(self.clientID) || self.clientID == oldSelf.clientID)",message="clientID is immutable"
// +kubebuilder:validation:XValidation:rule="!((has(self.public) && self.public) && has(self.secretRef))",message="secretRef must not be set on a public client"
// +kubebuilder:validation:XValidation:rule="!((has(self.public) && self.public) && has(self.generatedSecret))",message="generatedSecret must not be set on a public client"
// +kubebuilder:validation:XValidation:rule="!(has(self.secretRef) && has(self.generatedSecret))",message="secretRef and generatedSecret are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="(has(self.public) && self.public) || (has(self.redirectURIs) && size(self.redirectURIs) > 0)",message="redirectURIs is required unless the client is public"
type DexClientSpec struct {
	// ClientID is the OAuth2 client ID registered in dex. Defaults to
	// "<namespace>-<name>"; immutable once the object exists.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	ClientID string `json:"clientID,omitempty"`

	// Name is the human-readable client name dex shows on its approval
	// screen.
	// +optional
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`

	// Public marks a client that cannot keep a secret (a single-page app,
	// a CLI). Public clients have no secret.
	// +optional
	Public bool `json:"public,omitempty"`

	// RedirectURIs are the callback URLs dex may redirect to after login.
	// Required unless the client is public.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MaxLength=2048
	RedirectURIs []string `json:"redirectURIs,omitempty"`

	// PostLogoutRedirectURIs are where the browser may be sent after an
	// RP-initiated logout.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MaxLength=2048
	PostLogoutRedirectURIs []string `json:"postLogoutRedirectURIs,omitempty"`

	// TrustedPeers are client IDs allowed to mint tokens for this client.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MaxLength=253
	TrustedPeers []string `json:"trustedPeers,omitempty"`

	// AllowedConnectors restricts which dex connector IDs may be used to log
	// in to this client. Empty allows every connector.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MaxLength=253
	AllowedConnectors []string `json:"allowedConnectors,omitempty"`

	// LogoURL is shown on dex's approval screen.
	// +optional
	// +kubebuilder:validation:MaxLength=2048
	LogoURL string `json:"logoURL,omitempty"`

	// BackchannelLogoutURI is where dex POSTs a logout token when a session
	// this client took part in ends (OIDC Back-Channel Logout 1.0).
	// +optional
	// +kubebuilder:validation:MaxLength=2048
	BackchannelLogoutURI string `json:"backchannelLogoutURI,omitempty"`

	// RefreshTokenLifetime is whether this client's refresh tokens outlive
	// the browser session that issued them ("standalone", dex's default when
	// empty) or end with it ("session").
	// +optional
	// +kubebuilder:validation:Enum="";standalone;session
	RefreshTokenLifetime string `json:"refreshTokenLifetime,omitempty"`

	// SecretRef points at an existing Secret holding the client secret.
	// When unset (and the client is not public) the controller generates a
	// secret instead; see GeneratedSecret.
	// +optional
	SecretRef *ClientSecretReference `json:"secretRef,omitempty"`

	// GeneratedSecret configures the Secret the controller generates when
	// SecretRef is unset. The controller owns it (an owner reference ties
	// its lifetime to this object) and writes the keys clientID,
	// clientSecret and, when the controller knows dex's issuer, issuer.
	// +optional
	GeneratedSecret *GeneratedSecret `json:"generatedSecret,omitempty"`
}

// ClientSecretReference names the key of a Secret in the DexClient's
// namespace that holds the client secret.
type ClientSecretReference struct {
	// Name of the Secret.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`
	// Key within the Secret's data.
	// +optional
	// +kubebuilder:default=clientSecret
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Key string `json:"key,omitempty"`
}

// GeneratedSecret configures a controller-generated client Secret.
type GeneratedSecret struct {
	// Name of the Secret. Defaults to "<name>-dex-client".
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
}

// DexClientStatus is the observed state of a DexClient.
type DexClientStatus struct {
	// ClientID is the client ID registered in dex.
	// +optional
	ClientID string `json:"clientID,omitempty"`
	// SecretName is the Secret holding the client secret: SecretRef's, or
	// the generated one. Empty for a public client.
	// +optional
	SecretName string `json:"secretName,omitempty"`
	// ObservedGeneration is the generation the conditions describe.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions carry the Ready summary.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// DexClient registers one OAuth2 client in dex.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories=dex
// +kubebuilder:printcolumn:name="ID",type=string,JSONPath=`.status.clientID`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type DexClient struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DexClientSpec   `json:"spec"`
	Status DexClientStatus `json:"status,omitempty"`
}

// DexClientList is a list of DexClients.
//
// +kubebuilder:object:root=true
type DexClientList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DexClient `json:"items"`
}

// DexID is the client ID this object registers in dex: spec.clientID, or
// "<namespace>-<name>" when unset.
func (c *DexClient) DexID() string {
	if c.Spec.ClientID != "" {
		return c.Spec.ClientID
	}
	return defaultID(c.Namespace, c.Name)
}

// GeneratedSecretName is the name of the Secret the controller generates for
// this client when spec.secretRef is unset.
func (c *DexClient) GeneratedSecretName() string {
	if c.Spec.GeneratedSecret != nil && c.Spec.GeneratedSecret.Name != "" {
		return c.Spec.GeneratedSecret.Name
	}
	return c.Name + "-dex-client"
}

// SecretName is the Secret holding this client's secret, or "" for a public
// client.
func (c *DexClient) SecretName() string {
	switch {
	case c.Spec.Public:
		return ""
	case c.Spec.SecretRef != nil:
		return c.Spec.SecretRef.Name
	default:
		return c.GeneratedSecretName()
	}
}

// SecretKey is the key within SecretName holding the client secret.
func (c *DexClient) SecretKey() string {
	if c.Spec.SecretRef != nil && c.Spec.SecretRef.Key != "" {
		return c.Spec.SecretRef.Key
	}
	return GeneratedSecretKeyClientSecret
}
