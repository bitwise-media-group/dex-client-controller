// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package dextest is an in-memory stand-in for dex's gRPC API, implementing
// dex.Client with the semantics of dex's own API server
// (server/apiserver/{clients,connectors}.go and storage/static.go upstream):
//
//   - CreateClient generates a secret for a confidential client that names
//     none, and fails on a static client's ID;
//   - UpdateClient cannot touch secret or public, cannot clear name or
//     logoURL, and cannot clear a list (an empty repeated field is absent on
//     the wire), but can clear the two optional strings;
//   - writes to a static client or connector fail with dex's read-only
//     error;
//   - the connector RPCs fail while the CRUD feature flag is off, require
//     valid JSON config and known grant types, and bump the connector's
//     resource version on every update.
//
// Errors cross as gRPC code Unknown carrying dex's message, exactly as dex's
// plain Go errors do through grpc-go.
package dextest

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"

	api "github.com/dexidp/dex/api/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// grantTypes are the grant types dex lets a connector be restricted to
// (server/connectors/policy.go upstream).
var grantTypes = map[string]bool{
	"authorization_code": true,
	"refresh_token":      true,
	"implicit":           true,
	"password":           true,
	"urn:ietf:params:oauth:grant-type:device_code":    true,
	"urn:ietf:params:oauth:grant-type:token-exchange": true,
}

// Server is the fake. The zero value is not usable; call New.
type Server struct {
	mu sync.Mutex

	// ConnectorsCRUD is dex's DEX_API_CONNECTORS_CRUD feature flag.
	ConnectorsCRUD bool
	// Err, when set, is returned by every RPC (e.g. a codes.Unavailable
	// status to simulate dex being down).
	Err error

	clients          map[string]*api.Client
	staticClients    map[string]*api.Client
	connectors       map[string]*connector
	staticConnectors map[string]*connector
	calls            map[string]int
	nextID           int
}

type connector struct {
	c               *api.Connector
	resourceVersion int
}

// New returns an empty fake with the connector CRUD flag on.
func New() *Server {
	return &Server{
		ConnectorsCRUD:   true,
		clients:          map[string]*api.Client{},
		staticClients:    map[string]*api.Client{},
		connectors:       map[string]*connector{},
		staticConnectors: map[string]*connector{},
		calls:            map[string]int{},
	}
}

// AddStaticClient registers a client as if from dex's staticClients config.
func (s *Server) AddStaticClient(c *api.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staticClients[c.Id] = proto.Clone(c).(*api.Client)
}

// AddStaticConnector registers a connector as if from dex's connectors
// config.
func (s *Server) AddStaticConnector(c *api.Connector) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staticConnectors[c.Id] = &connector{c: proto.Clone(c).(*api.Connector), resourceVersion: 1}
}

// Client returns a copy of the stored (non-static) client, or nil.
func (s *Server) Client(id string) *api.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.clients[id]; ok {
		return proto.Clone(c).(*api.Client)
	}
	return nil
}

// Connector returns a copy of the stored (non-static) connector and its
// resource version, or nil.
func (s *Server) Connector(id string) (*api.Connector, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.connectors[id]; ok {
		return proto.Clone(c.c).(*api.Connector), c.resourceVersion
	}
	return nil, 0
}

// Wipe drops every stored client and connector, as a restart of a dex on
// memory storage does. Static entries survive.
func (s *Server) Wipe() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients = map[string]*api.Client{}
	s.connectors = map[string]*connector{}
}

// Calls returns how many times the named RPC (e.g. "UpdateConnector") was
// called.
func (s *Server) Calls(rpc string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[rpc]
}

// ResetCalls zeroes the RPC counters.
func (s *Server) ResetCalls() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = map[string]int{}
}

// enter counts the call and returns the injected error, if any. The caller
// holds s.mu.
func (s *Server) enter(rpc string) error {
	s.calls[rpc]++
	return s.Err
}

// dexErr is how a plain Go error from dex's API server arrives at a client.
func dexErr(format string, args ...any) error {
	return status.Error(codes.Unknown, fmt.Sprintf(format, args...))
}

func (s *Server) newID() string {
	s.nextID++
	return "generated-" + strconv.Itoa(s.nextID)
}

func validateLifetime(l string) error {
	switch l {
	case "", "standalone", "session":
		return nil
	}
	return dexErr("invalid refresh token lifetime %q: want %q or %q", l, "standalone", "session")
}

// GetClient implements dex.Client.
func (s *Server) GetClient(_ context.Context, in *api.GetClientReq, _ ...grpc.CallOption) (*api.GetClientResp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter("GetClient"); err != nil {
		return nil, err
	}
	if c, ok := s.staticClients[in.Id]; ok {
		return &api.GetClientResp{Client: proto.Clone(c).(*api.Client)}, nil
	}
	c, ok := s.clients[in.Id]
	if !ok {
		return nil, dexErr("not found")
	}
	return &api.GetClientResp{Client: proto.Clone(c).(*api.Client)}, nil
}

// CreateClient implements dex.Client.
func (s *Server) CreateClient(
	_ context.Context, in *api.CreateClientReq, _ ...grpc.CallOption,
) (*api.CreateClientResp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter("CreateClient"); err != nil {
		return nil, err
	}
	if in.Client == nil {
		return nil, dexErr("no client supplied")
	}
	c := proto.Clone(in.Client).(*api.Client)
	if c.Id == "" {
		c.Id = s.newID()
	}
	if c.Secret == "" && !c.Public {
		c.Secret = s.newID() + s.newID()
	}
	if err := validateLifetime(c.RefreshTokenLifetime); err != nil {
		return nil, err
	}
	if _, ok := s.staticClients[c.Id]; ok {
		return nil, dexErr("create client: static clients: read-only cannot create client")
	}
	if _, ok := s.clients[c.Id]; ok {
		return &api.CreateClientResp{AlreadyExists: true}, nil
	}
	s.clients[c.Id] = c
	return &api.CreateClientResp{Client: proto.Clone(c).(*api.Client)}, nil
}

// UpdateClient implements dex.Client.
func (s *Server) UpdateClient(
	_ context.Context, in *api.UpdateClientReq, _ ...grpc.CallOption,
) (*api.UpdateClientResp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter("UpdateClient"); err != nil {
		return nil, err
	}
	if in.Id == "" {
		return nil, dexErr("update client: no client ID supplied")
	}
	if err := validateLifetime(in.GetRefreshTokenLifetime()); err != nil {
		return nil, err
	}
	if _, ok := s.staticClients[in.Id]; ok {
		return nil, dexErr("update client: static clients: read-only cannot update client")
	}
	c, ok := s.clients[in.Id]
	if !ok {
		return &api.UpdateClientResp{NotFound: true}, nil
	}
	// len() rather than != nil: an empty repeated field is indistinguishable
	// from an absent one once it has crossed the wire.
	if len(in.RedirectUris) > 0 {
		c.RedirectUris = in.RedirectUris
	}
	if len(in.TrustedPeers) > 0 {
		c.TrustedPeers = in.TrustedPeers
	}
	if in.Name != "" {
		c.Name = in.Name
	}
	if in.LogoUrl != "" {
		c.LogoUrl = in.LogoUrl
	}
	if len(in.AllowedConnectors) > 0 {
		c.AllowedConnectors = in.AllowedConnectors
	}
	if len(in.SsoSharedWith) > 0 {
		c.SsoSharedWith = in.SsoSharedWith
	}
	if in.BackchannelLogoutUri != nil {
		c.BackchannelLogoutUri = in.GetBackchannelLogoutUri()
	}
	if len(in.PostLogoutRedirectUris) > 0 {
		c.PostLogoutRedirectUris = in.PostLogoutRedirectUris
	}
	if in.RefreshTokenLifetime != nil {
		c.RefreshTokenLifetime = in.GetRefreshTokenLifetime()
	}
	return &api.UpdateClientResp{}, nil
}

// DeleteClient implements dex.Client.
func (s *Server) DeleteClient(
	_ context.Context, in *api.DeleteClientReq, _ ...grpc.CallOption,
) (*api.DeleteClientResp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter("DeleteClient"); err != nil {
		return nil, err
	}
	if _, ok := s.staticClients[in.Id]; ok {
		return nil, dexErr("delete client: static clients: read-only cannot delete client")
	}
	if _, ok := s.clients[in.Id]; !ok {
		return &api.DeleteClientResp{NotFound: true}, nil
	}
	delete(s.clients, in.Id)
	return &api.DeleteClientResp{}, nil
}

func (s *Server) checkFlag() error {
	if !s.ConnectorsCRUD {
		return dexErr("api_connectors_crud feature flag is not enabled")
	}
	return nil
}

func checkGrantTypes(gts []string) error {
	for _, gt := range gts {
		if !grantTypes[gt] {
			return dexErr("unknown grant type %q", gt)
		}
	}
	return nil
}

// ListConnectors implements dex.Client.
func (s *Server) ListConnectors(
	_ context.Context, _ *api.ListConnectorReq, _ ...grpc.CallOption,
) (*api.ListConnectorResp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter("ListConnectors"); err != nil {
		return nil, err
	}
	if err := s.checkFlag(); err != nil {
		return nil, err
	}
	out := make([]*api.Connector, 0, len(s.connectors)+len(s.staticConnectors))
	for id, c := range s.connectors {
		if _, static := s.staticConnectors[id]; !static {
			out = append(out, proto.Clone(c.c).(*api.Connector))
		}
	}
	for _, c := range s.staticConnectors {
		out = append(out, proto.Clone(c.c).(*api.Connector))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Id < out[j].Id })
	return &api.ListConnectorResp{Connectors: out}, nil
}

// CreateConnector implements dex.Client.
func (s *Server) CreateConnector(
	_ context.Context, in *api.CreateConnectorReq, _ ...grpc.CallOption,
) (*api.CreateConnectorResp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter("CreateConnector"); err != nil {
		return nil, err
	}
	if err := s.checkFlag(); err != nil {
		return nil, err
	}
	c := in.Connector
	switch {
	case c == nil:
		return nil, dexErr("no connector supplied")
	case c.Id == "":
		return nil, dexErr("no id supplied")
	case c.Type == "":
		return nil, dexErr("no type supplied")
	case c.Name == "":
		return nil, dexErr("no name supplied")
	case len(c.Config) == 0:
		return nil, dexErr("no config supplied")
	case !json.Valid(c.Config):
		return nil, dexErr("invalid config supplied")
	}
	if err := checkGrantTypes(c.GrantTypes); err != nil {
		return nil, err
	}
	if _, ok := s.staticConnectors[c.Id]; ok {
		return nil, dexErr("api: failed to create connector: static connectors: read-only cannot create connector")
	}
	if _, ok := s.connectors[c.Id]; ok {
		return &api.CreateConnectorResp{AlreadyExists: true}, nil
	}
	s.connectors[c.Id] = &connector{c: proto.Clone(c).(*api.Connector), resourceVersion: 1}
	return &api.CreateConnectorResp{}, nil
}

// UpdateConnector implements dex.Client.
func (s *Server) UpdateConnector(
	_ context.Context, in *api.UpdateConnectorReq, _ ...grpc.CallOption,
) (*api.UpdateConnectorResp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter("UpdateConnector"); err != nil {
		return nil, err
	}
	if err := s.checkFlag(); err != nil {
		return nil, err
	}
	if in.Id == "" {
		return nil, dexErr("no email supplied")
	}
	if len(in.NewConfig) == 0 && in.NewName == "" && in.NewType == "" && in.NewGrantTypes == nil {
		return nil, dexErr("nothing to update")
	}
	if len(in.NewConfig) != 0 && !json.Valid(in.NewConfig) {
		return nil, dexErr("invalid config supplied")
	}
	if in.NewGrantTypes != nil {
		if err := checkGrantTypes(in.NewGrantTypes.GrantTypes); err != nil {
			return nil, err
		}
	}
	if _, ok := s.staticConnectors[in.Id]; ok {
		return nil, dexErr("update connector: static connectors: read-only cannot update connector")
	}
	c, ok := s.connectors[in.Id]
	if !ok {
		return &api.UpdateConnectorResp{NotFound: true}, nil
	}
	if in.NewType != "" {
		c.c.Type = in.NewType
	}
	if in.NewName != "" {
		c.c.Name = in.NewName
	}
	if len(in.NewConfig) != 0 {
		c.c.Config = in.NewConfig
	}
	if in.NewGrantTypes != nil {
		c.c.GrantTypes = in.NewGrantTypes.GrantTypes
	}
	c.resourceVersion++
	return &api.UpdateConnectorResp{}, nil
}

// DeleteConnector implements dex.Client.
func (s *Server) DeleteConnector(
	_ context.Context, in *api.DeleteConnectorReq, _ ...grpc.CallOption,
) (*api.DeleteConnectorResp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter("DeleteConnector"); err != nil {
		return nil, err
	}
	if err := s.checkFlag(); err != nil {
		return nil, err
	}
	if in.Id == "" {
		return nil, dexErr("no id supplied")
	}
	if _, ok := s.staticConnectors[in.Id]; ok {
		return nil, dexErr("delete connector: static connectors: read-only cannot delete connector")
	}
	if _, ok := s.connectors[in.Id]; !ok {
		return &api.DeleteConnectorResp{NotFound: true}, nil
	}
	delete(s.connectors, in.Id)
	return &api.DeleteConnectorResp{}, nil
}

// GetVersion implements dex.Client.
func (s *Server) GetVersion(_ context.Context, _ *api.VersionReq, _ ...grpc.CallOption) (*api.VersionResp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enter("GetVersion"); err != nil {
		return nil, err
	}
	return &api.VersionResp{Server: "dextest", Api: 2}, nil
}

// ErrUnavailable is a convenient Err value: what grpc-go returns when dex
// cannot be reached.
var ErrUnavailable = status.Error(codes.Unavailable, "connection refused")
