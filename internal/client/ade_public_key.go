// SPDX-License-Identifier: MPL-2.0

package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"net/http"
)

const adePublicKeyPath = "/api/v1/integrations/apple/ade/public_key/"

// ADEPublicKey contains only the public PEM and the digest of the exact HTTP
// response bytes. The digest identifies the response, not a normalized key.
type ADEPublicKey struct {
	PEM    string
	SHA256 string
}

// GetADEPublicKey reads the tenant's public ABM bootstrap material. Parsing
// establishes syntax and supported public-key encoding, not certificate trust,
// validity dates, chain membership, or suitability for a particular ABM account.
func (c *Client) GetADEPublicKey(ctx context.Context) (ADEPublicKey, error) {
	data, err := c.request(ctx, http.MethodGet, adePublicKeyPath, nil, http.StatusOK)
	if err != nil {
		return ADEPublicKey{}, err
	}
	return decodeADEPublicKey(data)
}

func decodeADEPublicKey(data []byte) (ADEPublicKey, error) {
	invalid := errors.New("Iru ADE public key response must contain one supported public certificate/key PEM block without other content (response body withheld)")
	trimmed := bytes.TrimSpace(data)
	block, rest := pem.Decode(trimmed)
	// pem.Decode skips leading text and malformed blocks. Refuse that leniency,
	// as well as multiple blocks, PEM headers, and appended/private material.
	if block == nil || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 ||
		!bytes.HasPrefix(trimmed, []byte("-----BEGIN "+block.Type+"-----")) ||
		bytes.Count(trimmed, []byte("-----BEGIN ")) != 1 || bytes.Count(trimmed, []byte("-----END ")) != 1 {
		return ADEPublicKey{}, invalid
	}
	switch block.Type {
	case "CERTIFICATE":
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil || certificate.PublicKey == nil {
			return ADEPublicKey{}, invalid
		}
	case "PUBLIC KEY":
		if _, err := x509.ParsePKIXPublicKey(block.Bytes); err != nil {
			return ADEPublicKey{}, invalid
		}
	case "RSA PUBLIC KEY":
		if _, err := x509.ParsePKCS1PublicKey(block.Bytes); err != nil {
			return ADEPublicKey{}, invalid
		}
	default:
		return ADEPublicKey{}, invalid
	}
	sum := sha256.Sum256(data)
	return ADEPublicKey{PEM: string(data), SHA256: hex.EncodeToString(sum[:])}, nil
}
