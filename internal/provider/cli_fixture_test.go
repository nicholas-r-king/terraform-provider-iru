// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Reuses the test-only provider entrypoint with a fixed synthetic origin/token.
func mockTerraform(t *testing.T, handler http.Handler) (func(int, ...string) string, func(string)) {
	t.Helper()
	tf := os.Getenv("IRU_TEST_TERRAFORM")
	if tf == "" {
		t.Skip("IRU_TEST_TERRAFORM is required for loopback-only CLI tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	dir := t.TempDir()
	plugins := filepath.Join(dir, "plugins")
	if err := os.Mkdir(plugins, 0700); err != nil {
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
	name := "terraform-provider-iru"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(plugins, name), binary, 0700); err != nil {
		t.Fatal(err)
	}
	rc := filepath.Join(dir, "terraform.tfrc")
	if err := os.WriteFile(rc, []byte(fmt.Sprintf("provider_installation { dev_overrides { %q = %q } }\n", "registry.terraform.io/nicholas-r-king/iru", filepath.ToSlash(plugins))), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	env := []string{}
	for _, value := range os.Environ() {
		key := strings.ToUpper(strings.SplitN(value, "=", 2)[0])
		if strings.HasPrefix(key, "IRU_") || strings.HasPrefix(key, "IRUCTL_") || strings.HasPrefix(key, "TF_") || key == "CHECKPOINT_DISABLE" {
			continue
		}
		env = append(env, value)
	}
	env = append(env, "IRU_SYNTHETIC_PROVIDER_HELPER=1", "IRU_SYNTHETIC_ENDPOINT="+server.URL, "IRU_API_URL=https://synthetic.api.iru.com", "IRU_API_TOKEN="+cliToken, "TF_CLI_CONFIG_FILE="+rc, "TF_IN_AUTOMATION=1", "TF_INPUT=0", "CHECKPOINT_DISABLE=1")
	run := func(want int, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, tf, args...)
		cmd.Dir, cmd.Env = dir, env
		output, err := cmd.CombinedOutput()
		if strings.Contains(string(output), cliToken) {
			t.Fatal("synthetic credential leaked to CLI output")
		}
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatal("Terraform could not execute")
			}
			code = exit.ExitCode()
		}
		if code != want {
			t.Fatalf("Terraform %v exit=%d want=%d: %s", args, code, want, output)
		}
		return string(output)
	}
	write := func(resource string) {
		t.Helper()
		config := "terraform {\n required_providers {\n  iru = { source = \"nicholas-r-king/iru\" }\n }\n}\nprovider \"iru\" {}\n" + resource
		if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return run, write
}
