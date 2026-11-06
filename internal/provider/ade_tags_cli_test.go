// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestTerraformCLIADETagsPreserveUnmanagedAssignmentsAndRecover(t *testing.T) {
	var mu sync.Mutex
	object := map[string]any{"id": syntheticID, "blueprint_id": syntheticCategory, "user_id": json.Number("9007199254740993"), "asset_tag": "keep", "use_blueprint_routing": false, "override_dep_profile_id": nil, "tags": []string{"unmanaged"}}
	mode := ""
	patches, otherWrites := 0, 0
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/integrations/apple/ade/devices/"+syntheticID {
			t.Error("wrong ADE target")
			w.WriteHeader(405)
			return
		}
		if r.Method == "PATCH" {
			patches++
			var fields map[string]json.RawMessage
			if json.NewDecoder(r.Body).Decode(&fields) != nil || len(fields) != 1 {
				t.Error("unmanaged fields reached PATCH")
				w.WriteHeader(400)
				return
			}
			if raw, ok := fields["tags"]; ok {
				var tags []string
				if json.Unmarshal(raw, &tags) != nil || tags == nil {
					t.Error("tags were not an explicit full list")
					w.WriteHeader(400)
					return
				}
				if mode != "rejected-503" {
					object["tags"] = tags
				}
			} else if raw, ok := fields["asset_tag"]; ok {
				var asset string
				_ = json.Unmarshal(raw, &asset)
				object["asset_tag"] = asset
			} else {
				t.Error("undocumented settings write")
				w.WriteHeader(400)
				return
			}
			if mode == "rejected-503" || mode == "committed-503" {
				w.WriteHeader(503)
				return
			}
			if mode == "missing-after-write" {
				mode = "missing-tags"
			}
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if r.Method != "GET" {
			otherWrites++
			w.WriteHeader(405)
			return
		}
		copy := map[string]any{}
		for k, v := range object {
			copy[k] = v
		}
		if mode == "missing-tags" {
			delete(copy, "tags")
		}
		if mode == "null-tags" {
			copy["tags"] = nil
		}
		_ = json.NewEncoder(w).Encode(copy)
	}))
	config := func(fields string) {
		write(fmt.Sprintf("resource \"iru_ade_device_settings\" \"example\" {\n device_id=%q\n%s\n}\n", syntheticID, fields))
	}
	config("")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	if patches != 0 {
		t.Error("omitted ADE settings were written")
	}
	mu.Unlock()
	pull := func() map[string]any {
		t.Helper()
		var value struct {
			Resources []struct {
				Instances []struct{ Attributes map[string]any }
			}
		}
		if json.Unmarshal([]byte(run(0, "state", "pull")), &value) != nil || len(value.Resources) != 1 || len(value.Resources[0].Instances) != 1 {
			t.Fatal("settings identity missing")
		}
		attrs := value.Resources[0].Instances[0].Attributes
		if attrs["id"] != syntheticID || attrs["user_id"] != "9007199254740993" {
			t.Fatal("legacy user observation or locator lost")
		}
		return attrs
	}
	if tags, ok := pull()["tags"].([]any); !ok || len(tags) != 1 || tags[0] != "unmanaged" {
		t.Fatal("omitted tags were not observed")
	}
	// Simulate an older local state without the additive optional/computed field.
	var legacy map[string]any
	_ = json.Unmarshal([]byte(run(0, "state", "pull")), &legacy)
	resources := legacy["resources"].([]any)
	instances := resources[0].(map[string]any)["instances"].([]any)
	delete(instances[0].(map[string]any)["attributes"].(map[string]any), "tags")
	legacy["serial"] = legacy["serial"].(float64) + 1
	raw, _ := json.Marshal(legacy)
	file := filepath.Join(t.TempDir(), "synthetic-legacy-state.json")
	if os.WriteFile(file, raw, 0600) != nil {
		t.Fatal("legacy state fixture failed")
	}
	run(0, "state", "push", file)
	run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
	pull()
	config(` tags=["beta","alpha"]`)
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	tags := append([]string{}, object["tags"].([]string)...)
	sort.Strings(tags)
	if strings.Join(tags, ",") != "alpha,beta" || patches != 1 {
		t.Error("full tag set not written exactly once")
	}
	mu.Unlock()
	run(0, "state", "rm", "iru_ade_device_settings.example")
	run(0, "import", "-no-color", "iru_ade_device_settings.example", syntheticID)
	run(0, "plan", "-no-color", "-detailed-exitcode")
	before := run(0, "state", "pull")
	for _, failure := range []string{"missing-tags", "null-tags"} {
		mu.Lock()
		mode = failure
		mu.Unlock()
		run(1, "plan", "-no-color")
		if run(0, "state", "pull") != before {
			t.Fatal("lost ADE tags erased cached state")
		}
	}
	mu.Lock()
	mode = ""
	mu.Unlock()
	for _, failure := range []string{"rejected-503", "committed-503", "missing-after-write"} {
		config(fmt.Sprintf(" tags=[%q]", failure))
		before = run(0, "state", "pull")
		mu.Lock()
		mode = failure
		was := patches
		mu.Unlock()
		run(1, "apply", "-auto-approve", "-no-color")
		if run(0, "state", "pull") != before {
			t.Fatal("failed tag update changed persisted prior state")
		}
		mu.Lock()
		if patches != was+1 {
			t.Error("ambiguous tag write replayed")
		}
		mode = ""
		mu.Unlock()
		run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
		pull()
		// Finish the explicitly configured synthetic transition after observing it.
		run(0, "apply", "-auto-approve", "-no-color")
		run(0, "plan", "-no-color", "-detailed-exitcode")
	}
	config(" tags=[]")
	run(0, "apply", "-auto-approve", "-no-color")
	if tags, ok := pull()["tags"].([]any); !ok || len(tags) != 0 {
		t.Fatal("explicit empty tags did not clear")
	}
	mu.Lock()
	object["tags"] = []string{"externally-restored"}
	mu.Unlock()
	config(` asset_tag="changed"`)
	run(0, "apply", "-auto-approve", "-no-color")
	if tags, ok := pull()["tags"].([]any); !ok || len(tags) != 1 || tags[0] != "externally-restored" {
		t.Fatal("omitting tags during another update cleared them")
	}
	mu.Lock()
	was := patches
	mu.Unlock()
	run(0, "destroy", "-auto-approve", "-no-color")
	mu.Lock()
	defer mu.Unlock()
	if patches != was || otherWrites != 0 || object["user_id"] != json.Number("9007199254740993") || object["blueprint_id"] != syntheticCategory {
		t.Fatal("release or tags touched unmanaged assignments/device lifecycle")
	}
}

func TestTerraformCLIADETagsUnavailableStopsBeforeWrite(t *testing.T) {
	var writes atomic.Int64
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes.Add(1)
			w.WriteHeader(405)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": syntheticID, "blueprint_id": syntheticCategory, "user_id": 42, "asset_tag": nil, "use_blueprint_routing": false})
	}))
	write(fmt.Sprintf("resource \"iru_ade_device_settings\" \"example\" {\n device_id=%q\n tags=[\"new\"]\n}\n", syntheticID))
	if !strings.Contains(run(1, "apply", "-auto-approve", "-no-color"), "does not report ADE tags") || writes.Load() != 0 {
		t.Fatal("unobservable ADE tags were written or assumed empty")
	}
	write(fmt.Sprintf("resource \"iru_ade_device_settings\" \"example\" {\n device_id=%q\n user_id=\"42\"\n}\n", syntheticID))
	run(1, "validate", "-no-color")
}

func TestTerraformCLIDeviceUnknownTagMemberDefers(t *testing.T) {
	for _, kind := range []string{"device_settings", "ade_device_settings"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int64
			run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(405) }))
			write(fmt.Sprintf("resource \"terraform_data\" \"tag\" { input=\"later\" }\nresource %q \"example\" {\n device_id=%q\n tags=[terraform_data.tag.output]\n}\n", "iru_"+kind, syntheticID))
			run(2, "plan", "-no-color", "-detailed-exitcode")
			if calls.Load() != 0 {
				t.Fatal("unknown tag member reached API")
			}
		})
	}
}

func TestTerraformCLIADETagsMetadataIsReadOnlyAndPreservesUnknownEmpty(t *testing.T) {
	var mu sync.Mutex
	mode := "missing"
	writes := 0
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != "GET" || r.URL.Path != "/api/v1/integrations/apple/ade/devices/"+syntheticID {
			writes++
			w.WriteHeader(405)
			return
		}
		value := map[string]any{"id": syntheticID, "blueprint_id": syntheticCategory, "user_id": 42, "asset_tag": nil, "use_blueprint_routing": false, "serial_number": "synthetic-private-serial"}
		switch mode {
		case "empty":
			value["tags"] = []string{}
		case "names":
			value["tags"] = []string{"beta", "alpha"}
		case "null":
			value["tags"] = nil
		case "duplicate":
			value["tags"] = []string{"alpha", "alpha"}
		}
		_ = json.NewEncoder(w).Encode(value)
	}))
	write(fmt.Sprintf("data \"iru_ade_device\" \"example\" { device_id=%q }\n", syntheticID))
	run(0, "apply", "-auto-approve", "-no-color")
	if raw := run(0, "show", "-json"); !strings.Contains(raw, `"tags":null`) || !strings.Contains(raw, `"user_id":"42"`) || strings.Contains(raw, "synthetic-private") {
		t.Fatal("missing tags or legacy metadata projection changed")
	}
	mu.Lock()
	mode = "empty"
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	if !strings.Contains(run(0, "show", "-json"), `"tags":[]`) {
		t.Fatal("observed empty tags became unknown")
	}
	mu.Lock()
	mode = "names"
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	before := run(0, "state", "pull")
	for _, bad := range []string{"null", "duplicate"} {
		mu.Lock()
		mode = bad
		mu.Unlock()
		run(1, "plan", "-no-color")
		if run(0, "state", "pull") != before {
			t.Fatal("failed tag metadata read changed cached state")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if writes != 0 {
		t.Fatal("metadata lookup attempted mutation")
	}
}
