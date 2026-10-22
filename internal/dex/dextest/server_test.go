// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package dextest

import (
	"context"
	"testing"

	api "github.com/dexidp/dex/api/v2"

	"github.com/bitwise-media-group/dex-client-controller/internal/dex"
)

// The fake is only useful while it behaves like dex; these pin the quirks
// the reconcilers depend on.

func TestClientSemantics(t *testing.T) {
	ctx := context.Background()
	s := New()

	resp, err := s.CreateClient(ctx, &api.CreateClientReq{Client: &api.Client{
		Id: "app", Name: "App", TrustedPeers: []string{"peer"}, BackchannelLogoutUri: "https://bc",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Client.Secret == "" {
		t.Error("CreateClient did not generate a secret for a confidential client")
	}
	if again, _ := s.CreateClient(ctx, &api.CreateClientReq{Client: &api.Client{Id: "app"}}); !again.AlreadyExists {
		t.Error("second CreateClient did not report AlreadyExists")
	}

	empty := ""
	if _, err := s.UpdateClient(ctx, &api.UpdateClientReq{
		Id: "app", Name: "", TrustedPeers: []string{}, BackchannelLogoutUri: &empty,
	}); err != nil {
		t.Fatal(err)
	}
	c := s.Client("app")
	if c.Name != "App" || len(c.TrustedPeers) != 1 {
		t.Errorf("UpdateClient cleared name/list (%v); dex cannot", c)
	}
	if c.BackchannelLogoutUri != "" {
		t.Error("UpdateClient did not clear the optional backchannel URI")
	}

	if _, err := s.GetClient(ctx, &api.GetClientReq{Id: "absent"}); !dex.IsNotFound(err) {
		t.Errorf("GetClient(absent) = %v, want dex's not found", err)
	}
	s.AddStaticClient(&api.Client{Id: "static"})
	if _, err := s.DeleteClient(ctx, &api.DeleteClientReq{Id: "static"}); !dex.IsReadOnly(err) {
		t.Errorf("DeleteClient(static) = %v, want read-only", err)
	}
}

func TestConnectorSemantics(t *testing.T) {
	ctx := context.Background()
	s := New()
	s.ConnectorsCRUD = false
	if _, err := s.ListConnectors(ctx, &api.ListConnectorReq{}); !dex.IsFeatureDisabled(err) {
		t.Fatalf("ListConnectors with the flag off = %v, want the feature-flag error", err)
	}
	s.ConnectorsCRUD = true

	if _, err := s.CreateConnector(ctx, &api.CreateConnectorReq{Connector: &api.Connector{
		Id: "c", Type: "oidc", Name: "C", Config: []byte("{not json"),
	}}); err == nil {
		t.Error("CreateConnector accepted invalid JSON")
	}
	if _, err := s.CreateConnector(ctx, &api.CreateConnectorReq{Connector: &api.Connector{
		Id: "c", Type: "oidc", Name: "C", Config: []byte(`{}`),
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateConnector(ctx, &api.UpdateConnectorReq{Id: "c"}); err == nil {
		t.Error("UpdateConnector with nothing to update succeeded")
	}
	if _, err := s.UpdateConnector(ctx, &api.UpdateConnectorReq{Id: "c", NewName: "D"}); err != nil {
		t.Fatal(err)
	}
	if c, rv := s.Connector("c"); c.Name != "D" || rv != 2 {
		t.Errorf("after update: %v rv=%d, want name D and resource version 2", c, rv)
	}
	s.AddStaticConnector(&api.Connector{Id: "static", Type: "mockCallback", Name: "M", Config: []byte(`{}`)})
	list, err := s.ListConnectors(ctx, &api.ListConnectorReq{})
	if err != nil || len(list.Connectors) != 2 {
		t.Fatalf("ListConnectors = %v, %v; want the dynamic and the static connector", list, err)
	}
	if _, err := s.UpdateConnector(ctx, &api.UpdateConnectorReq{Id: "static", NewName: "X"}); !dex.IsReadOnly(err) {
		t.Errorf("UpdateConnector(static) = %v, want read-only", err)
	}
}
