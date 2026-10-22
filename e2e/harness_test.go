// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	api "github.com/dexidp/dex/api/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/bitwise-media-group/dex-client-controller/internal/dex"
	"github.com/bitwise-media-group/dex-client-controller/internal/dex/dextest"
	"github.com/bitwise-media-group/dex-client-controller/internal/kube"
)

// env is the shared world every test drives: one kube-apiserver, two dex
// processes (connector CRUD on and off) and one controller process per dex,
// each confined to its own namespace.
type env struct {
	dir   string
	certs dextest.Certs
	kube  client.Client
	// dex is the API of the dex running with DEX_API_CONNECTORS_CRUD=true.
	dex dex.Client
	// issuer is that dex's issuer URL.
	issuer string
}

const (
	// nsApps is reconciled by the controller pointed at the CRUD-enabled dex.
	nsApps = "apps"
	// nsNoFlag is reconciled by the controller pointed at the dex without
	// the connector CRUD flag.
	nsNoFlag = "noflag"
	// staticClientID is a client defined in dex's config file.
	staticClientID = "static-app"
)

var (
	setupOnce sync.Once
	shared    *env
	setupErr  error
	cleanups  []func()
)

func TestMain(m *testing.M) {
	code := m.Run()
	for i := len(cleanups) - 1; i >= 0; i-- {
		cleanups[i]()
	}
	os.Exit(code)
}

// world boots the shared environment on first use. Skipped without
// KUBEBUILDER_ASSETS (hack/e2e.sh sets it).
func world(t *testing.T) *env {
	t.Helper()
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; run via `mise run e2e`")
	}
	setupOnce.Do(func() { shared, setupErr = setup() })
	if setupErr != nil {
		t.Fatalf("e2e setup: %v", setupErr)
	}
	return shared
}

func setup() (*env, error) {
	dir, err := os.MkdirTemp("", "dcc-e2e-")
	if err != nil {
		return nil, err
	}
	cleanups = append(cleanups, func() { _ = os.RemoveAll(dir) })
	e := &env{dir: dir}

	if e.certs, err = dextest.WriteCerts(dir); err != nil {
		return nil, err
	}

	// Build both binaries: dex from the module's `tool` directive, the
	// controller from the parent module (replace ../). CGO off, as in the
	// release build: dex then lacks only its sqlite storage, and memory
	// storage is all this suite uses.
	dexBin, ctrlBin := filepath.Join(dir, "dex"), filepath.Join(dir, "dex-client-controller")
	for bin, pkg := range map[string]string{
		dexBin:  "github.com/dexidp/dex/cmd/dex",
		ctrlBin: "github.com/bitwise-media-group/dex-client-controller/cmd/dex-client-controller",
	} {
		cmd := exec.Command("go", "build", "-o", bin, pkg)
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("go build %s: %v\n%s", pkg, err, out)
		}
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{"../config/crd"},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	if err != nil {
		return nil, fmt.Errorf("start envtest: %w", err)
	}
	cleanups = append(cleanups, func() { _ = testEnv.Stop() })
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, testEnv.KubeConfig, 0o600); err != nil {
		return nil, err
	}
	if e.kube, err = client.New(cfg, client.Options{Scheme: kube.Scheme()}); err != nil {
		return nil, err
	}
	for _, ns := range []string{nsApps, nsNoFlag} {
		if err := e.kube.Create(context.Background(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns},
		}); err != nil {
			return nil, err
		}
	}

	grpcAddr, issuer, err := e.startDex(dexBin, "crud", true)
	if err != nil {
		return nil, err
	}
	e.issuer = issuer
	if e.dex, err = e.dial(grpcAddr); err != nil {
		return nil, err
	}
	noFlagAddr, _, err := e.startDex(dexBin, "noflag", false)
	if err != nil {
		return nil, err
	}

	// A long resync: every assertion below must be driven by watch events
	// (CR and Secret changes), not by the periodic drift repair.
	if err := e.startController(ctrlBin, kubeconfig, "apps", grpcAddr, issuer, nsApps); err != nil {
		return nil, err
	}
	if err := e.startController(ctrlBin, kubeconfig, "noflag", noFlagAddr, "", nsNoFlag); err != nil {
		return nil, err
	}
	return e, nil
}

// startDex runs dex on memory storage with an mTLS gRPC listener, one static
// client, and the connector CRUD flag on or off.
func (e *env) startDex(bin, name string, crud bool) (grpcAddr, issuer string, err error) {
	webAddr, err := freeAddr()
	if err != nil {
		return "", "", err
	}
	if grpcAddr, err = freeAddr(); err != nil {
		return "", "", err
	}
	issuer = "http://" + webAddr + "/dex"
	config := fmt.Sprintf(`issuer: %s
storage:
  type: memory
web:
  http: %s
grpc:
  addr: %s
  tlsCert: %s
  tlsKey: %s
  tlsClientCA: %s
logger:
  level: warn
connectors:
  - type: mockCallback
    id: mock
    name: Mock
staticClients:
  - id: %s
    secret: from-config
    name: Static
    redirectURIs: ["https://static.example.com/callback"]
`, issuer, webAddr, grpcAddr, e.certs.ServerCert, e.certs.ServerKey, e.certs.CA, staticClientID)
	path := filepath.Join(e.dir, "dex-"+name+".yaml")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		return "", "", err
	}
	cmd := exec.Command(bin, "serve", path)
	cmd.Env = append(os.Environ(), "DEX_API_CONNECTORS_CRUD="+strconv.FormatBool(crud))
	if err := e.start(cmd, "dex-"+name); err != nil {
		return "", "", err
	}
	c, err := e.dial(grpcAddr)
	if err != nil {
		return "", "", err
	}
	err = poll(30*time.Second, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := c.GetVersion(ctx, &api.VersionReq{})
		return err
	})
	if err != nil {
		return "", "", fmt.Errorf("dex %s never answered GetVersion: %w\n%s", name, err, e.logTail("dex-"+name))
	}
	return grpcAddr, issuer, nil
}

func (e *env) startController(bin, kubeconfig, name, dexAddr, issuer, namespace string) error {
	healthAddr, err := freeAddr()
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, "serve",
		"--kubeconfig="+kubeconfig,
		"--leader-elect=false",
		"--metrics-addr=",
		"--health-addr="+healthAddr,
		"--watch-namespaces="+namespace,
		"--resync-period=1h",
		"--dex-address="+dexAddr,
		"--dex-ca-file="+e.certs.CA,
		"--dex-cert-file="+e.certs.ClientCert,
		"--dex-key-file="+e.certs.ClientKey,
		"--dex-issuer="+issuer,
		"--log-level=debug",
	)
	if err := e.start(cmd, "controller-"+name); err != nil {
		return err
	}
	// /readyz includes the dex GetVersion check: ready means the controller
	// reaches dex over mTLS.
	err = poll(60*time.Second, func() error {
		resp, err := http.Get("http://" + healthAddr + "/readyz")
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("readyz: %s", resp.Status)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("controller %s never became ready: %w\n%s", name, err, e.logTail("controller-"+name))
	}
	return nil
}

// start runs cmd with its output in <dir>/<name>.log and stops it at exit.
func (e *env) start(cmd *exec.Cmd, name string) error {
	log, err := os.Create(filepath.Join(e.dir, name+".log"))
	if err != nil {
		return err
	}
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}
	cleanups = append(cleanups, func() {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
		}
		_ = log.Close()
	})
	return nil
}

func (e *env) dial(addr string) (dex.Client, error) {
	c, closeFn, err := dex.Dial(dex.DialOptions{
		Address:  addr,
		CAFile:   e.certs.CA,
		CertFile: e.certs.ClientCert,
		KeyFile:  e.certs.ClientKey,
	})
	if err != nil {
		return nil, err
	}
	cleanups = append(cleanups, func() { _ = closeFn() })
	return c, nil
}

// logTail returns the last lines of a process log, for failure messages.
func (e *env) logTail(name string) string {
	b, err := os.ReadFile(filepath.Join(e.dir, name+".log"))
	if err != nil {
		return ""
	}
	lines := bytes.Split(bytes.TrimSpace(b), []byte("\n"))
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}
	return "--- " + name + " log ---\n" + string(bytes.Join(lines, []byte("\n")))
}

// eventually polls fn until it succeeds, failing the test with the last
// error and the controller logs otherwise.
func (e *env) eventually(t *testing.T, what string, fn func() error) {
	t.Helper()
	if err := poll(30*time.Second, fn); err != nil {
		t.Fatalf("%s: %v\n%s\n%s", what, err, e.logTail("controller-apps"), e.logTail("controller-noflag"))
	}
}

func poll(timeout time.Duration, fn func() error) error {
	deadline := time.Now().Add(timeout)
	for {
		err := fn()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// freeAddr returns a localhost address with a port that was free a moment
// ago.
func freeAddr() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().String(), nil
}

