package net

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
)

func loadTLSConfig(opts *tlsOptions) (*tls.Config, error) {
	if opts.CertFilename == "" || opts.KeyFilename == "" {
		return nil, errors.New("certfilename and keyfilename are required")
	}

	certPEM, err := os.ReadFile(opts.CertFilename)
	if err != nil {
		return nil, fmt.Errorf("read certificate file: %w", err)
	}
	keyPEM, err := os.ReadFile(opts.KeyFilename)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	if opts.Passphrase != nil && *opts.Passphrase != "" {
		block, rest := pem.Decode(keyPEM)
		if block == nil {
			return nil, errors.New("key file does not contain a PEM block")
		}
		if x509.IsEncryptedPEMBlock(block) {
			decrypted, err := x509.DecryptPEMBlock(block, []byte(*opts.Passphrase))
			if err != nil {
				return nil, fmt.Errorf("decrypt key file: %w", err)
			}
			keyPEM = append(pem.EncodeToMemory(&pem.Block{Type: block.Type, Bytes: decrypted}), rest...)
		}
	}

	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("load certificate and key: %w", err)
	}
	config := &tls.Config{Certificates: []tls.Certificate{certificate}}
	if opts.CAFilename != "" {
		caPEM, err := os.ReadFile(opts.CAFilename)
		if err != nil {
			return nil, fmt.Errorf("read CA file: %w", err)
		}
		clientCAs := x509.NewCertPool()
		if !clientCAs.AppendCertsFromPEM(caPEM) {
			return nil, errors.New("CA file contains no valid certificates")
		}
		config.ClientCAs = clientCAs
		config.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return config, nil
}
