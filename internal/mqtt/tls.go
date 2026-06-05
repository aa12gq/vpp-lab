package mqtt

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

type TLSFiles struct {
	CAFile             string
	CertFile           string
	KeyFile            string
	InsecureSkipVerify bool
}

func NewTLSConfig(files TLSFiles) (*tls.Config, error) {
	if files.CAFile == "" && files.CertFile == "" && files.KeyFile == "" && !files.InsecureSkipVerify {
		return nil, nil
	}

	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: files.InsecureSkipVerify, //nolint:gosec // Explicit lab opt-in for local/self-signed brokers.
	}

	if files.CAFile != "" {
		caPEM, err := os.ReadFile(files.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read mqtt ca file: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("parse mqtt ca file: no certificates found")
		}
		cfg.RootCAs = roots
	}

	if files.CertFile != "" || files.KeyFile != "" {
		if files.CertFile == "" || files.KeyFile == "" {
			return nil, fmt.Errorf("mqtt client certificate and key must be configured together")
		}
		cert, err := tls.LoadX509KeyPair(files.CertFile, files.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load mqtt client certificate: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}

	return cfg, nil
}
