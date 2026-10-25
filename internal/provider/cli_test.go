package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

const cliToken = "synthetic-cli-token"

// The helper exists ONLY in the Go test binary. The production executable has
// no helper flags/environment handling and always uses its normal transport.
func TestMain(m *testing.M) {
	if os.Getenv("IRU_SYNTHETIC_PROVIDER_HELPER") != "1" {
		os.Exit(m.Run())
	}
	u, err := url.Parse(os.Getenv("IRU_SYNTHETIC_ENDPOINT"))
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		fmt.Fprintln(os.Stderr, "invalid synthetic loopback endpoint")
		os.Exit(1)
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil || host != "127.0.0.1" || port == "" {
		fmt.Fprintln(os.Stderr, "synthetic endpoint must use IPv4 loopback")
		os.Exit(1)
	}
	direct := &http.Transport{Proxy: nil}
	transport := syntheticTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Scheme != "https" || req.URL.Host != "synthetic.api.iru.com" || req.Header.Get("Authorization") != "Bearer "+cliToken {
			return nil, errors.New("test helper refuses nonsynthetic origin or credentials")
		}
		clone := req.Clone(req.Context())
		copyURL := *req.URL
		clone.URL = &copyURL
		clone.URL.Scheme, clone.URL.Host, clone.Host = "http", u.Host, u.Host
		return direct.RoundTrip(clone)
	})
	factory := func() fwprovider.Provider {
		return &IruProvider{version: "synthetic-test", httpClient: &http.Client{Transport: transport}}
	}
	if err := providerserver.Serve(context.Background(), factory, providerserver.ServeOpts{Address: "registry.terraform.io/nicholas-r-king/iru"}); err != nil {
		fmt.Fprintln(os.Stderr, "synthetic provider server failed")
		os.Exit(1)
	}
	os.Exit(0)
}

// TestTerraformCLI uses a caller-supplied official CLI and only a local mock.
// It never downloads a CLI, reads real credentials, or contacts an Iru endpoint.
func TestTerraformCLI(t *testing.T) {
	tf := os.Getenv("IRU_TEST_TERRAFORM")
	if tf == "" {
		t.Skip("set IRU_TEST_TERRAFORM to a verified local Terraform executable for loopback-only CLI tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")
	if err := os.Mkdir(pluginDir, 0700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "terraform-provider-iru.exe"), binary, 0700); err != nil {
		t.Fatal(err)
	}
	rc := filepath.Join(dir, "terraform.tfrc")
	debugLog := filepath.Join(dir, "terraform-debug.log")
	config := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    \"nicholas-r-king/iru\" = %q\n  }\n}\n", filepath.ToSlash(pluginDir))
	if err := os.WriteFile(rc, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	model := syntheticModel()
	var mu sync.Mutex
	object := &client.Script{ID: syntheticID, ScriptInput: model.input()}
	methods := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if req.Header.Get("Authorization") != "Bearer "+cliToken {
			t.Error("missing synthetic bearer authentication")
			w.WriteHeader(401)
			return
		}
		if req.URL.Path != "/api/v1/library/custom-scripts" && req.URL.Path != "/api/v1/library/custom-scripts/"+syntheticID {
			t.Error("unexpected mock API path")
			w.WriteHeader(404)
			return
		}
		methods[req.Method]++
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "GET":
			if object == nil {
				w.WriteHeader(404)
				return
			}
		case "POST", "PATCH":
			if req.Method == "PATCH" && object == nil {
				w.WriteHeader(404)
				return
			}
			var input client.ScriptInput
			if json.NewDecoder(req.Body).Decode(&input) != nil || input.Validate() != nil {
				t.Error("invalid write reached mock")
				w.WriteHeader(400)
				return
			}
			object = &client.Script{ID: syntheticID, ScriptInput: input}
			if req.Method == "POST" {
				w.WriteHeader(201)
			}
		case "DELETE":
			object = nil
			w.WriteHeader(204)
			return
		default:
			t.Error("unsupported mock request")
			w.WriteHeader(405)
			return
		}
		if err := json.NewEncoder(w).Encode(object); err != nil {
			t.Error("mock response encoding failed")
		}
	}))
	defer server.Close()
	env := []string{}
	for _, value := range os.Environ() {
		key := strings.ToUpper(strings.SplitN(value, "=", 2)[0])
		if strings.HasPrefix(key, "IRU_") || strings.HasPrefix(key, "IRUCTL_") || strings.HasPrefix(key, "TF_") || key == "CHECKPOINT_DISABLE" {
			continue
		}
		env = append(env, value)
	}
	env = append(env, "IRU_SYNTHETIC_PROVIDER_HELPER=1", "IRU_SYNTHETIC_ENDPOINT="+server.URL, "IRU_API_URL=https://synthetic.api.iru.com", "IRU_API_TOKEN="+cliToken, "TF_CLI_CONFIG_FILE="+rc, "TF_IN_AUTOMATION=1", "TF_INPUT=0", "CHECKPOINT_DISABLE=1", "TF_LOG=DEBUG", "TF_LOG_PATH="+debugLog)
	run := func(want int, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, tf, args...)
		cmd.Dir = dir
		cmd.Env = env
		output, err := cmd.CombinedOutput()
		text := string(output)
		if strings.Contains(text, cliToken) {
			t.Fatal("Terraform output exposed synthetic credential")
		}
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatalf("Terraform could not run: %v", err)
			}
			code = exit.ExitCode()
		}
		if code != want {
			log, _ := os.ReadFile(debugLog)
			lines := strings.Split(strings.ReplaceAll(string(log), cliToken, "[redacted]"), "\n")
			if len(lines) > 35 {
				lines = lines[len(lines)-35:]
			}
			t.Fatalf("terraform %v exit=%d want=%d:\n%s\nSynthetic debug tail:\n%s", args, code, want, text, strings.Join(lines, "\n"))
		}
		return text
	}
	writeConfig := func(name, frequency string) {
		t.Helper()
		hcl := fmt.Sprintf("terraform {\n required_providers {\n  iru = { source = \"nicholas-r-king/iru\" }\n }\n}\nprovider \"iru\" {}\nresource \"iru_custom_script\" \"example\" {\n name = %q\n script = \"#!/bin/sh\\nexit 0\\n\"\n execution_frequency = %q\n active = false\n restart = false\n}\n", name, frequency)
		if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(hcl), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeConfig("Synthetic audit", "once")
	run(0, "validate", "-no-color")
	schemaOutput := run(0, "providers", "schema", "-json")
	var schema map[string]any
	if json.Unmarshal([]byte(schemaOutput), &schema) != nil {
		t.Fatal("CLI schema was not JSON")
	}
	run(0, "import", "-no-color", "iru_custom_script.example", strings.ToUpper(syntheticID))
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	postsAfterImport := methods["POST"]
	mu.Unlock()
	if postsAfterImport != 0 {
		t.Fatal("import/no-op unexpectedly created a script")
	}
	writeConfig("Synthetic updated", "once")
	run(2, "plan", "-no-color", "-detailed-exitcode")
	run(0, "apply", "-no-color", "-auto-approve")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	run(0, "destroy", "-no-color", "-auto-approve")
	run(0, "apply", "-no-color", "-auto-approve")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	object = nil
	mu.Unlock()
	run(2, "plan", "-no-color", "-detailed-exitcode")
	run(0, "apply", "-no-color", "-refresh-only", "-auto-approve")
	if strings.TrimSpace(run(0, "state", "list")) != "" {
		t.Fatal("404 refresh did not remove synthetic resource")
	}
	writeConfig("Synthetic audit", "hourly")
	run(1, "validate", "-no-color")
	mu.Lock()
	defer mu.Unlock()
	for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
		if methods[method] == 0 {
			t.Errorf("CLI did not exercise %s", method)
		}
	}
	log, err := os.ReadFile(debugLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(log), cliToken) {
		t.Fatal("synthetic authentication leaked to Terraform debug logs")
	}
}
