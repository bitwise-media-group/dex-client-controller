// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package v1alpha1 defines the dex.bitwisemedia.uk/v1alpha1 API: namespaced
// custom resources an application team creates to self-serve SSO against a
// shared dex, without editing dex's central configuration.
//
//   - DexClient is one OAuth2 client registered in dex. Its secret comes from
//     a Secret the team owns (spec.secretRef) or one the controller
//     generates and owns.
//   - DexConnector is one upstream identity provider dex federates to. Its
//     config is free-form JSON (the connector type's own schema); secret
//     values are merged in from Secrets at dotted paths.
//
// Both kinds default their dex ID to "<namespace>-<name>" so two namespaces
// cannot collide, and the ID is immutable once set.
package v1alpha1
