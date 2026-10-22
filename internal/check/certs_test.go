package check

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// selfSigned returns a PEM encoded certificate that expires at notAfter.
func selfSigned(t *testing.T, host string, notAfter time.Time) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    notAfter.Add(-90 * 24 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func tlsSecret(ns, name string, crt []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Type:       corev1.SecretTypeTLS,
		Data:       map[string][]byte{corev1.TLSCertKey: crt},
	}
}

func TestCertificateCheck(t *testing.T) {
	opaque := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "app", Name: "db-password"},
		Type:       corev1.SecretTypeOpaque,
	}
	fs := runCheck(t, CertificateCheck{}, testOptions(),
		tlsSecret("ingress", "valid", selfSigned(t, "app.example.com", testNow.Add(80*24*time.Hour))),
		tlsSecret("ingress", "soon", selfSigned(t, "soon.example.com", testNow.Add(10*24*time.Hour))),
		tlsSecret("ingress", "expired", selfSigned(t, "old.example.com", testNow.Add(-24*time.Hour))),
		tlsSecret("ingress", "garbage", []byte("not a certificate")),
		tlsSecret("ingress", "empty", nil),
		opaque,
	)
	if f := expectStatus(t, fs, "secret/ingress/valid", StatusPass); f.Message != "app.example.com valid for 80 days" {
		t.Errorf("message = %q", f.Message)
	}
	if f := expectStatus(t, fs, "secret/ingress/soon", StatusWarn); f.Message != "soon.example.com expires in 10 days" {
		t.Errorf("message = %q", f.Message)
	}
	expectStatus(t, fs, "secret/ingress/expired", StatusFail)
	expectStatus(t, fs, "secret/ingress/garbage", StatusFail)
	expectStatus(t, fs, "secret/ingress/empty", StatusFail)
	if _, ok := byResource(fs)["secret/app/db-password"]; ok {
		t.Error("non-TLS secrets must not be inspected")
	}
}

func TestLeafCertificateSkipsNonCertificateBlocks(t *testing.T) {
	bundle := append(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: []byte("x")}),
		selfSigned(t, "leaf.example.com", testNow.Add(time.Hour))...)
	cert, err := leafCertificate(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "leaf.example.com" {
		t.Fatalf("got %s", cert.Subject.CommonName)
	}
}
