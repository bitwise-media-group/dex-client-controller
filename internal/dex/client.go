// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package dex

import (
	"context"

	api "github.com/dexidp/dex/api/v2"
	"google.golang.org/grpc"
)

// Client is the subset of api.DexClient the reconcilers use. The generated
// gRPC client satisfies it directly; dextest.Server is the in-memory fake.
type Client interface {
	GetClient(ctx context.Context, in *api.GetClientReq, opts ...grpc.CallOption) (*api.GetClientResp, error)
	CreateClient(ctx context.Context, in *api.CreateClientReq, opts ...grpc.CallOption) (*api.CreateClientResp, error)
	UpdateClient(ctx context.Context, in *api.UpdateClientReq, opts ...grpc.CallOption) (*api.UpdateClientResp, error)
	DeleteClient(ctx context.Context, in *api.DeleteClientReq, opts ...grpc.CallOption) (*api.DeleteClientResp, error)

	ListConnectors(ctx context.Context, in *api.ListConnectorReq,
		opts ...grpc.CallOption) (*api.ListConnectorResp, error)
	CreateConnector(ctx context.Context, in *api.CreateConnectorReq,
		opts ...grpc.CallOption) (*api.CreateConnectorResp, error)
	UpdateConnector(ctx context.Context, in *api.UpdateConnectorReq,
		opts ...grpc.CallOption) (*api.UpdateConnectorResp, error)
	DeleteConnector(ctx context.Context, in *api.DeleteConnectorReq,
		opts ...grpc.CallOption) (*api.DeleteConnectorResp, error)

	GetVersion(ctx context.Context, in *api.VersionReq, opts ...grpc.CallOption) (*api.VersionResp, error)
}

// The generated client must keep satisfying Client.
var _ Client = api.NewDexClient(nil)
