// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package dextest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Certs are the PEM files of a throwaway mTLS setup, as dex's grpc block and
// the controller's --dex-* flags consume them.
type Certs struct {
	// CA signs both leaf certificates: dex's tlsClientCA and the
	// controller's --dex-ca-file.
	CA string
	// ServerCert/ServerKey are dex's tlsCert/tlsKey, valid for localhost
	// and 127.0.0.1.
	ServerCert, ServerKey string
	// ClientCert/ClientKey are the controller's --dex-cert-file and
	// --dex-key-file.
	ClientCert, ClientKey string
}

// WriteCerts generates a CA plus server and client certificates into dir.
func WriteCerts(dir string) (Certs, error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Certs{}, err
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "dextest CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return Certs{}, err
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return Certs{}, err
	}

	c := Certs{
		CA:         filepath.Join(dir, "ca.crt"),
		ServerCert: filepath.Join(dir, "server.crt"),
		ServerKey:  filepath.Join(dir, "server.key"),
		ClientCert: filepath.Join(dir, "client.crt"),
		ClientKey:  filepath.Join(dir, "client.key"),
	}
	if err := writePEM(c.CA, "CERTIFICATE", caDER); err != nil {
		return Certs{}, err
	}
	server := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if err := writeLeaf(server, caCert, caKey, c.ServerCert, c.ServerKey); err != nil {
		return Certs{}, err
	}
	client := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "dex-client-controller"},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	if err := writeLeaf(client, caCert, caKey, c.ClientCert, c.ClientKey); err != nil {
		return Certs{}, err
	}
	return c, nil
}

func writeLeaf(tmpl, ca *x509.Certificate, caKey *ecdsa.PrivateKey, certPath, keyPath string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	tmpl.NotBefore = time.Now().Add(-time.Hour)
	tmpl.NotAfter = time.Now().Add(24 * time.Hour)
	tmpl.KeyUsage = x509.KeyUsageDigitalSignature
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("sign %s: %w", tmpl.Subject.CommonName, err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	if err := writePEM(certPath, "CERTIFICATE", der); err != nil {
		return err
	}
	return writePEM(keyPath, "PRIVATE KEY", keyDER)
}

func writePEM(path, typ string, der []byte) error {
	return os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600)
}
