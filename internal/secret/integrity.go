package secret

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"
)

func (p Payload) validateIntegrity() error {
	now := time.Now()

	ca, err := parseCertificatePEM(p.CACertPEM)
	if err != nil {
		return fmt.Errorf("SECRET_KEY CA certificate: %w", err)
	}
	if err := validateCertificateTime(ca, now); err != nil {
		return fmt.Errorf("SECRET_KEY CA certificate: %w", err)
	}
	if err := ca.CheckSignature(ca.SignatureAlgorithm, ca.RawTBSCertificate, ca.Signature); err != nil {
		return fmt.Errorf("SECRET_KEY CA certificate is not self-signed: %w", err)
	}

	node, err := parseCertificatePEM(p.NodeCertPEM)
	if err != nil {
		return fmt.Errorf("SECRET_KEY node certificate: %w", err)
	}
	if err := ca.CheckSignature(node.SignatureAlgorithm, node.RawTBSCertificate, node.Signature); err != nil {
		return fmt.Errorf("SECRET_KEY node certificate was not signed by its CA: %w", err)
	}

	privateKey, err := parsePrivateKeyPEM(p.NodeKeyPEM)
	if err != nil {
		return fmt.Errorf("SECRET_KEY node private key: %w", err)
	}
	if err := publicKeysEqual(node.PublicKey, privateKey.Public()); err != nil {
		return fmt.Errorf("SECRET_KEY node private key does not match its certificate: %w", err)
	}

	if err := parsePublicKeyPEM(p.JWTPublicKey); err != nil {
		return fmt.Errorf("SECRET_KEY JWT public key: %w", err)
	}
	return nil
}

func parseCertificatePEM(pemText string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("PEM certificate could not be decoded")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	return certificate, nil
}

func validateCertificateTime(certificate *x509.Certificate, now time.Time) error {
	if now.Before(certificate.NotBefore) {
		return fmt.Errorf("is not valid before %s", certificate.NotBefore.Format(time.RFC3339))
	}
	if now.After(certificate.NotAfter) {
		return fmt.Errorf("expired at %s", certificate.NotAfter.Format(time.RFC3339))
	}
	return nil
}

func parsePrivateKeyPEM(pemText string) (crypto.Signer, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, errors.New("PEM private key could not be decoded")
	}

	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if signer, ok := key.(crypto.Signer); ok {
			return signer, nil
		}
		return nil, errors.New("PKCS#8 key cannot sign")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, errors.New("unsupported private key PEM")
}

func parsePublicKeyPEM(pemText string) error {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return errors.New("PEM public key could not be decoded")
	}
	if _, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		return nil
	}
	if _, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return nil
	}
	if _, err := x509.ParseCertificate(block.Bytes); err == nil {
		return nil
	}
	return errors.New("unsupported public key PEM")
}

func publicKeysEqual(left, right crypto.PublicKey) error {
	leftDER, err := x509.MarshalPKIXPublicKey(left)
	if err != nil {
		return fmt.Errorf("marshal certificate public key: %w", err)
	}
	rightDER, err := x509.MarshalPKIXPublicKey(right)
	if err != nil {
		return fmt.Errorf("marshal private-key public key: %w", err)
	}
	if !bytes.Equal(leftDER, rightDER) {
		return errors.New("public keys differ")
	}
	return nil
}
