package check

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
)

// CertificateCheck inspects kubernetes.io/tls Secrets and reports certificates
// that are expired, expiring soon, or cannot be parsed.
// It works whether certificates are issued by cert-manager or uploaded by hand.
type CertificateCheck struct{}

func (CertificateCheck) Name() string { return "certificates" }
func (CertificateCheck) Description() string {
	return "TLS Secrets contain a valid certificate that is not about to expire"
}

func (c CertificateCheck) Run(ctx context.Context, cs kubernetes.Interface, opt Options) ([]Finding, error) {
	secrets, err := cs.CoreV1().Secrets(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("type", string(corev1.SecretTypeTLS)).String(),
	})
	if err != nil {
		return nil, fmt.Errorf("list tls secrets: %w", err)
	}
	now := opt.now()
	warnBefore := time.Duration(opt.CertWarnDays) * 24 * time.Hour

	var out []Finding
	for _, s := range secrets.Items {
		// The fake clientset ignores field selectors, so filter again here.
		if s.Type != corev1.SecretTypeTLS || !opt.inScope(s.Namespace) {
			continue
		}
		res := "secret/" + s.Namespace + "/" + s.Name
		cert, err := leafCertificate(s.Data[corev1.TLSCertKey])
		if err != nil {
			out = append(out, fail(c.Name(), res, err.Error()))
			continue
		}
		left := cert.NotAfter.Sub(now)
		subject := cert.Subject.CommonName
		if len(cert.DNSNames) > 0 {
			subject = cert.DNSNames[0]
		}
		switch {
		case left <= 0:
			out = append(out, fail(c.Name(), res, fmt.Sprintf("%s expired on %s", subject, cert.NotAfter.Format(time.DateOnly))))
		case left <= warnBefore:
			out = append(out, warn(c.Name(), res, fmt.Sprintf("%s expires in %d days", subject, int(left.Hours()/24))))
		default:
			out = append(out, pass(c.Name(), res, fmt.Sprintf("%s valid for %d days", subject, int(left.Hours()/24))))
		}
	}
	return out, nil
}

// leafCertificate returns the first certificate in a PEM bundle.
func leafCertificate(data []byte) (*x509.Certificate, error) {
	if len(data) == 0 {
		return nil, errors.New("tls.crt is empty")
	}
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			return nil, errors.New("tls.crt has no PEM certificate")
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse tls.crt: %w", err)
		}
		return cert, nil
	}
}
