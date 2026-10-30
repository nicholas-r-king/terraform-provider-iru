// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestTerraformCLIADEPublicKey(t *testing.T) {
	publicDER, err := x509.MarshalPKIXPublicKey(ed25519.PublicKey(bytes.Repeat([]byte{7}, ed25519.PublicKeySize)))
	if err != nil {
		t.Fatal(err)
	}
	public := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
	var mu sync.Mutex
	body, status, gets := public, http.StatusOK, 0
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if req.Method != http.MethodGet || req.URL.Path != "/api/v1/integrations/apple/ade/public_key/" || req.URL.RawQuery != "" {
			t.Error("ADE public key lookup made a mutation or wrong request")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		gets++
		w.Header().Set("Content-Type", "application/x-x509-ca-cert")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	write("data \"iru_ade_public_key\" \"example\" {}\noutput \"bootstrap_key_sha256\" { value = data.iru_ade_public_key.example.sha256 }\n")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	var state struct {
		Values struct {
			RootModule struct {
				Resources []struct {
					Address string            `json:"address"`
					Values  map[string]string `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if err := json.Unmarshal([]byte(run(0, "show", "-json")), &state); err != nil {
		t.Fatal(err)
	}
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte(public)))
	if len(state.Values.RootModule.Resources) != 1 || state.Values.RootModule.Resources[0].Address != "data.iru_ade_public_key.example" {
		t.Fatal("public key data source state was missing")
	}
	values := state.Values.RootModule.Resources[0].Values
	if len(values) != 3 || values["public_key_pem"] != public || values["sha256"] != sum || values["id"] != sum {
		t.Fatal("public key state did not contain only the exact public PEM/digest identity")
	}
	mu.Lock()
	body = public + "\n"
	mu.Unlock()
	run(2, "plan", "-no-color", "-detailed-exitcode")
	run(0, "apply", "-auto-approve", "-no-color")
	mu.Lock()
	body = "-----BEGIN PRIVATE KEY-----\nc3ludGhldGljLXByaXZhdGUtbWF0ZXJpYWw=\n-----END PRIVATE KEY-----\n"
	mu.Unlock()
	failure := run(1, "plan", "-no-color")
	if strings.Contains(failure, "c3ludGhldGlj") || strings.Contains(failure, "-----BEGIN PRIVATE KEY-----") {
		t.Fatal("private response appeared in Terraform diagnostics")
	}
	mu.Lock()
	status, body = http.StatusNotFound, "synthetic-unrelated-private-message"
	mu.Unlock()
	failure = run(1, "plan", "-no-color")
	if strings.Contains(failure, "synthetic-unrelated-private-message") {
		t.Fatal("HTTP response appeared in Terraform diagnostics")
	}
	mu.Lock()
	defer mu.Unlock()
	if gets == 0 {
		t.Fatal("public key mock received no reads")
	}
}
