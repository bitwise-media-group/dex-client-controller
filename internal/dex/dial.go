// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package dex

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	api "github.com/dexidp/dex/api/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// DialOptions configure the connection to dex's gRPC API.
type DialOptions struct {
	// Address is dex's gRPC listener, host:port (dex's grpc.addr).
	Address string
	// CAFile verifies dex's serving certificate (dex's grpc.tlsCert).
	CAFile string
	// CertFile and KeyFile are the client certificate presented to dex,
	// which must chain to dex's grpc.tlsClientCA. dex's API has no other
	// authentication: whoever holds this certificate administers dex.
	CertFile string
	KeyFile  string
	// ServerName overrides the name verified in dex's certificate; empty
	// uses the host part of Address.
	ServerName string
	// Insecure dials without TLS. For local development only: it leaves
	// the API unauthenticated.
	Insecure bool
}

// Dial opens a lazily-connecting gRPC client to dex. The client certificate
// is re-read from disk on every TLS handshake, so a rotated Secret (e.g. by
// cert-manager) is picked up on the next reconnect without a restart.
func Dial(opts DialOptions) (api.DexClient, func() error, error) {
	if opts.Address == "" {
		return nil, nil, errors.New("dex address is required")
	}
	creds, err := transportCredentials(opts)
	if err != nil {
		return nil, nil, err
	}
	conn, err := grpc.NewClient(opts.Address, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, nil, fmt.Errorf("dial dex %s: %w", opts.Address, err)
	}
	return api.NewDexClient(conn), conn.Close, nil
}

func transportCredentials(opts DialOptions) (credentials.TransportCredentials, error) {
	if opts.Insecure {
		return insecure.NewCredentials(), nil
	}
	cfg, err := TLSConfig(opts)
	if err != nil {
		return nil, err
	}
	return credentials.NewTLS(cfg), nil
}

// TLSConfig builds the mTLS client configuration from DialOptions.
func TLSConfig(opts DialOptions) (*tls.Config, error) {
	if opts.CertFile == "" || opts.KeyFile == "" {
		return nil, errors.New("dex client certificate and key are required (dex's API is authenticated by mTLS only)")
	}
	// Fail fast on an unreadable key pair rather than at the first
	// handshake.
	if _, err := tls.LoadX509KeyPair(opts.CertFile, opts.KeyFile); err != nil {
		return nil, fmt.Errorf("dex client certificate: %w", err)
	}
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: opts.ServerName,
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			cert, err := tls.LoadX509KeyPair(opts.CertFile, opts.KeyFile)
			if err != nil {
				return nil, fmt.Errorf("dex client certificate: %w", err)
			}
			return &cert, nil
		},
	}
	if opts.CAFile != "" {
		pem, err := os.ReadFile(opts.CAFile)
		if err != nil {
			return nil, fmt.Errorf("dex CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("dex CA %s: no PEM certificates", opts.CAFile)
		}
		cfg.RootCAs = pool
	}
	return cfg, nil
}
