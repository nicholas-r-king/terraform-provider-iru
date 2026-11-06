// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRunnerCommandDeadlineAndExitStatus(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r := runner{root: t.TempDir(), env: cleanEnv(os.Environ())}
	args := []string{"-test.run=^TestRunnerDeadlineHelper$"}
	_, err = r.runWithTimeout(100*time.Millisecond, true, []string{"IRU_TOOL_TEST_HELPER=wait"}, exe, args...)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "100ms command deadline") {
		t.Fatalf("deadline must explain cancellation: %v", err)
	}
	_, err = r.runWithTimeout(time.Minute, true, []string{"IRU_TOOL_TEST_HELPER=fail"}, exe, args...)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ordinary exit must retain its status: %v", err)
	}
	output, err := r.runWithTimeout(time.Minute, true, []string{"IRU_TOOL_TEST_HELPER=ok"}, exe, args...)
	if err != nil || string(output) != "completed" {
		t.Fatalf("successful command capture changed: %q, %v", output, err)
	}
}

func TestRunnerDeadlineHelper(t *testing.T) {
	switch os.Getenv("IRU_TOOL_TEST_HELPER") {
	case "wait":
		time.Sleep(time.Minute)
		os.Exit(0)
	case "fail":
		os.Exit(3)
	case "ok":
		fmt.Print("completed")
		os.Exit(0)
	}
}

func TestChildEnvironmentIsolation(t *testing.T) {
	input := []string{"PATH=/synthetic/tools", "GOMODCACHE=/synthetic/cache", "IRU_API_TOKEN=synthetic-private", "iructl_token=synthetic-private", "TF_CLI_ARGS=-synthetic", "TF_LOG=DEBUG", "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=1", "GOTOOLCHAIN=auto", "CHECKPOINT_DISABLE=0"}
	original := append([]string{}, input...)
	actual := cleanEnv(input)
	want := []string{"PATH=/synthetic/tools", "GOMODCACHE=/synthetic/cache", "GOTOOLCHAIN=local", "CGO_ENABLED=0", "CHECKPOINT_DISABLE=1"}
	if !reflect.DeepEqual(actual, want) || !reflect.DeepEqual(input, original) {
		t.Fatal("child environment must isolate credentials/overrides without changing the caller")
	}
}

func TestDocumentationAliasRetainsSchema(t *testing.T) {
	provider := json.RawMessage(`{"provider":{"block":{"attributes":{"api_token":{"sensitive":true}}}},"resource_schemas":{"iru_custom_script":{"version":0}}}`)
	document, _ := json.Marshal(map[string]any{"format_version": "1.0", "provider_schemas": map[string]json.RawMessage{providerAddress: provider}})
	aliased, err := aliasSchema(document)
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Format  string                     `json:"format_version"`
		Schemas map[string]json.RawMessage `json:"provider_schemas"`
	}
	if json.Unmarshal(aliased, &output) != nil || output.Format != "1.0" || len(output.Schemas) != 1 || !bytes.Equal(output.Schemas["iru"], provider) {
		t.Fatal("documentation alias changed provider fields, sensitive flags or format version")
	}
	for _, invalid := range []string{"not json", `{}`, strings.ReplaceAll(string(document), providerAddress, "registry.terraform.io/synthetic/other")} {
		if _, err := aliasSchema([]byte(invalid)); err == nil {
			t.Fatal("invalid or unrelated schema accepted")
		}
	}
}
