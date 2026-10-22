// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package dex

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	api "github.com/dexidp/dex/api/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/bitwise-media-group/dex-client-controller/internal/dex/dextest"
)

// versionServer answers GetVersion only — enough to prove the handshake.
type versionServer struct {
	api.UnimplementedDexServer
}

func (versionServer) GetVersion(context.Context, *api.VersionReq) (*api.VersionResp, error) {
	return &api.VersionResp{Server: "test", Api: 2}, nil
}

// serveMTLS starts a gRPC server requiring client certificates signed by
// certs.CA, the way dex's grpc.tlsClientCA does, and returns its address.
func serveMTLS(t *testing.T, certs dextest.Certs) string {
	t.Helper()
	serverCert, err := tls.LoadX509KeyPair(certs.ServerCert, certs.ServerKey)
	if err != nil {
		t.Fatal(err)
	}
	caPEM, err := os.ReadFile(certs.CA)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)
	srv := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	})))
	api.RegisterDexServer(srv, versionServer{})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

func TestDialMTLS(t *testing.T) {
	certs, err := dextest.WriteCerts(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	addr := serveMTLS(t, certs)

	c, closeFn, err := Dial(DialOptions{
		Address:  addr,
		CAFile:   certs.CA,
		CertFile: certs.ClientCert,
		KeyFile:  certs.ClientKey,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = closeFn() }()
	check := ReadyCheck(c, 5*time.Second)
	if err := check(httptest.NewRequest("GET", "/readyz", nil)); err != nil {
		t.Fatalf("ReadyCheck with a valid client certificate: %v", err)
	}

	// A certificate from a different CA must be refused by the server.
	other, err := dextest.WriteCerts(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bad, closeBad, err := Dial(DialOptions{
		Address:  addr,
		CAFile:   certs.CA,
		CertFile: other.ClientCert,
		KeyFile:  other.ClientKey,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = closeBad() }()
	if err := ReadyCheck(bad, 5*time.Second)(httptest.NewRequest("GET", "/readyz", nil)); err == nil {
		t.Fatal("ReadyCheck with a foreign client certificate succeeded, want a handshake failure")
	}
}

func TestDialOptionsValidation(t *testing.T) {
	certs, err := dextest.WriteCerts(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		opts DialOptions
		ok   bool
	}{
		{"no address", DialOptions{Insecure: true}, false},
		{"insecure", DialOptions{Address: "127.0.0.1:1", Insecure: true}, true},
		{"no client certificate", DialOptions{Address: "127.0.0.1:1", CAFile: certs.CA}, false},
		{"missing CA file", DialOptions{
			Address: "127.0.0.1:1", CAFile: certs.CA + ".absent", CertFile: certs.ClientCert, KeyFile: certs.ClientKey,
		}, false},
		{"CA file without PEM", DialOptions{
			Address: "127.0.0.1:1", CAFile: certs.ClientKey + "x", CertFile: certs.ClientCert, KeyFile: certs.ClientKey,
		}, false},
		{"key does not match", DialOptions{
			Address: "127.0.0.1:1", CertFile: certs.ClientCert, KeyFile: certs.ServerKey,
		}, false},
		{"system roots", DialOptions{
			Address: "127.0.0.1:1", CertFile: certs.ClientCert, KeyFile: certs.ClientKey,
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, closeFn, err := Dial(tt.opts)
			if (err == nil) != tt.ok {
				t.Fatalf("Dial error = %v, want ok=%v", err, tt.ok)
			}
			if closeFn != nil {
				_ = closeFn()
			}
		})
	}
}
