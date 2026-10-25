// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

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
