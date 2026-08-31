package httpserver

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luxiaba/remnanode-lite/internal/auth"
	"github.com/luxiaba/remnanode-lite/internal/config"
	"github.com/luxiaba/remnanode-lite/internal/geocheck"
	"github.com/luxiaba/remnanode-lite/internal/nodehandler"
	"github.com/luxiaba/remnanode-lite/internal/secret"
)

func TestExternalServerSecurityPolicy(t *testing.T) {
	server := newSecurityTestServer(t)
	if got := server.httpServer.TLSConfig.MinVersion; got != tls.VersionTLS13 {
		t.Fatalf("minimum TLS version = %#x, want TLS 1.3", got)
	}
	if server.httpServer.TLSNextProto == nil || len(server.httpServer.TLSNextProto) != 0 {
		t.Fatal("HTTP/2 must be disabled to preserve connection-drop semantics")
	}
	if got := server.httpServer.MaxHeaderBytes; got != 64<<10 {
		t.Fatalf("MaxHeaderBytes = %d, want %d", got, 64<<10)
	}
	if got := server.httpServer.TLSConfig.ClientAuth; got != tls.RequireAndVerifyClientCert {
		t.Fatalf("client authentication = %v, want RequireAndVerifyClientCert", got)
	}
}

func TestTLSCertificateUsesDefaultCertificateWhenSNIVerificationIsDisabled(t *testing.T) {
	payload := testTLSPayload(t)
	tlsConfig, err := buildTLSConfig(payload, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(tlsConfig.Certificates) != 1 {
		t.Fatalf("default certificate count = %d, want 1", len(tlsConfig.Certificates))
	}
	if tlsConfig.GetCertificate != nil {
		t.Fatal("disabled SNI verification installed a certificate gate")
	}
}

func TestTLSCertificateRequiresExactDerivedSNIWhenEnabled(t *testing.T) {
	payload := testTLSPayload(t)
	tlsConfig, err := buildTLSConfig(payload, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(tlsConfig.Certificates) != 0 {
		t.Fatal("TLS config must not expose a default certificate")
	}
	if tlsConfig.GetCertificate == nil {
		t.Fatal("TLS config is missing its SNI certificate gate")
	}

	expected, err := secret.DeriveSNI(payload.CACertPEM, payload.JWTPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tlsConfig.GetCertificate(&tls.ClientHelloInfo{ServerName: expected})
	if err != nil || certificate == nil {
		t.Fatalf("derived SNI was rejected: certificate=%v error=%v", certificate != nil, err)
	}

	for _, offered := range []string{"", strings.ToUpper(expected), expected + ".", "x" + expected[1:]} {
		if certificate, err := tlsConfig.GetCertificate(&tls.ClientHelloInfo{ServerName: offered}); err == nil || certificate != nil {
			t.Fatalf("unexpected SNI %q returned certificate=%v error=%v", offered, certificate != nil, err)
		}
	}
}

func TestExternalServerDropsUnauthorizedAndUnknownRequests(t *testing.T) {
	server := newSecurityTestServer(t)
	testServer := httptest.NewServer(server.httpServer.Handler)
	defer testServer.Close()

	for _, path := range []string{"/node/xray/healthcheck", "/unknown"} {
		req, err := http.NewRequest(http.MethodGet, testServer.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := testServer.Client().Do(req)
		if response != nil {
			response.Body.Close()
			t.Fatalf("%s returned HTTP %s instead of dropping the connection", path, response.Status)
		}
		if err == nil {
			t.Fatalf("%s returned no client error after dropping the connection", path)
		}
	}
}

func newSecurityTestServer(t *testing.T) *Server {
	t.Helper()
	payload := testTLSPayload(t)
	validator, err := auth.NewJWTValidator(payload.JWTPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(config.Config{}, payload, securityTestDependencies(t, validator))
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func securityTestDependencies(t *testing.T, validator *auth.JWTValidator) Dependencies {
	t.Helper()
	var calls atomic.Int64
	return Dependencies{
		Validator: validator,
		Xray:      &recordingXrayController{},
		Stats:     newTestStatsService(countingStatsProvider{calls: &calls}),
		GeoCheck:  geocheck.NewService("geocheck"),
		Handler:   nodehandler.NewService(countingHandlerProvider{calls: &calls}, nil),
		Plugins:   &recordingPluginController{},
		Body:      newHTTPTestBudget(t, false, 0),
	}
}

func TestNewRejectsMissingDependencies(t *testing.T) {
	payload := testTLSPayload(t)
	validator, err := auth.NewJWTValidator(payload.JWTPublicKey)
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]func(*Dependencies){
		"JWT validator":       func(d *Dependencies) { d.Validator = nil },
		"Xray controller":     func(d *Dependencies) { d.Xray = nil },
		"stats service":       func(d *Dependencies) { d.Stats = nil },
		"geocheck service":    func(d *Dependencies) { d.GeoCheck = nil },
		"handler service":     func(d *Dependencies) { d.Handler = nil },
		"plugin controller":   func(d *Dependencies) { d.Plugins = nil },
		"request body budget": func(d *Dependencies) { d.Body = nil },
	}
	for name, remove := range tests {
		t.Run(name, func(t *testing.T) {
			dependencies := securityTestDependencies(t, validator)
			remove(&dependencies)
			if _, err := New(config.Config{}, payload, dependencies); err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("New error = %v, want missing %s", err, name)
			}
		})
	}
}

func testTLSPayload(t *testing.T) secret.Payload {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "remnanode-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}

	jwtKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwtDER, err := x509.MarshalPKIXPublicKey(&jwtKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
	return secret.Payload{
		CACertPEM:    certPEM,
		JWTPublicKey: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: jwtDER})),
		NodeCertPEM:  certPEM,
		NodeKeyPEM:   string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})),
	}
}
