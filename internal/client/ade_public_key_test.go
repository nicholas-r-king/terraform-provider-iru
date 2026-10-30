// SPDX-License-Identifier: MPL-2.0

package client

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"
)

func adePublicKeyFixtures(t *testing.T) map[string][]byte {
	t.Helper()
	// Deterministic synthetic material, unrelated to any real API/signing key.
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{42}, ed25519.SeedSize))
	publicDER, err := x509.MarshalPKIXPublicKey(private.Public())
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Synthetic ABM bootstrap"}, NotBefore: time.Unix(0, 0), NotAfter: time.Unix(1, 0)}
	certificateDER, err := x509.CreateCertificate(rand.Reader, certificate, certificate, private.Public(), private)
	if err != nil {
		t.Fatal(err)
	}
	// PKCS#1 parsing does not require a corresponding private key.
	rsaPublic := &rsa.PublicKey{N: new(big.Int).SetBytes(bytes.Repeat([]byte{0xa5}, 128)), E: 65537}
	return map[string][]byte{
		"certificate": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER}),
		"pkix":        pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}),
		"pkcs1":       pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(rsaPublic)}),
	}
}

func TestADEPublicKeyFormatsAndExactResponseIdentity(t *testing.T) {
	for name, fixture := range adePublicKeyFixtures(t) {
		t.Run(name, func(t *testing.T) {
			body := " \r\n" + string(fixture) + "\t\r\n"
			calls := 0
			c := testClient(t, func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != http.MethodGet || req.URL.Path != adePublicKeyPath || req.URL.RawQuery != "" || req.Header.Get("Authorization") != "Bearer "+testToken {
					t.Error("public key lookup made an incorrect or unauthenticated request")
				}
				return response(http.StatusOK, body), nil
			})
			value, err := c.GetADEPublicKey(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256([]byte(body))
			if calls != 1 || value.PEM != body || value.SHA256 != hex.EncodeToString(sum[:]) {
				t.Fatal("public PEM bytes or exact response digest changed")
			}
			trimmed, err := decodeADEPublicKey(fixture)
			if err != nil || trimmed.SHA256 == value.SHA256 {
				t.Fatal("response formatting was incorrectly omitted from the digest")
			}
		})
	}
}

func TestADEPublicKeyRejectsPrivateMalformedAndExtraMaterial(t *testing.T) {
	public := adePublicKeyFixtures(t)["pkix"]
	publicBlock, _ := pem.Decode(public)
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{43}, ed25519.SeedSize))
	privateDER, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
	fixtures := [][]byte{
		nil, []byte("synthetic-unrelated-response"), []byte("-----BEGIN PUBLIC KEY-----\ninvalid\n-----END PUBLIC KEY-----\n"),
		pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte("synthetic-invalid-der")}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: publicBlock.Bytes}),
		pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: publicBlock.Bytes}),
		pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Headers: map[string]string{"Comment": "synthetic"}, Bytes: publicBlock.Bytes}),
		privatePEM,
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privateDER}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privateDER}),
		pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: privateDER}),
		append(append([]byte{}, public...), privatePEM...),
		append(append([]byte{}, public...), public...),
		append(append([]byte{}, public...), []byte("synthetic-trailing-data")...),
		append([]byte("synthetic-leading-data\n"), public...),
		append([]byte("-----BEGIN PUBLIC KEY-----\nmalformed\n"), public...),
	}
	for i, fixture := range fixtures {
		value, err := decodeADEPublicKey(fixture)
		if err == nil || value != (ADEPublicKey{}) {
			t.Fatalf("invalid fixture %d exposed public key state", i)
		}
		if strings.Contains(err.Error(), "synthetic") || strings.Contains(err.Error(), string(privatePEM)) || strings.Contains(err.Error(), testToken) {
			t.Fatal("invalid response leaked into an error")
		}
	}
}

func TestADEPublicKeyHTTPFailuresStayErrors(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusFound} {
		c := testClient(t, func(*http.Request) (*http.Response, error) {
			return response(status, "synthetic-private-server-message"), nil
		})
		value, err := c.GetADEPublicKey(context.Background())
		if err == nil || value != (ADEPublicKey{}) || strings.Contains(err.Error(), "synthetic-private-server-message") {
			t.Fatal("HTTP failure became public key state or exposed response content")
		}
	}
}
