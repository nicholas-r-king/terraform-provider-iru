// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestTerraformCLILibraryItemStatusesSnapshotAndFailures(t *testing.T) {
	var mu sync.Mutex
	mode, reverse, transient := "", false, 0
	gets, writes := 0, 0
	path := "/api/v1/library/library-items/" + syntheticID + "/status"
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "0")
		if r.Method != "GET" {
			writes++
			w.WriteHeader(405)
			return
		}
		gets++
		if r.URL.Path != path || r.Header.Get("Authorization") != "Bearer "+cliToken {
			t.Error("status data source escaped its exact GET scope")
			w.WriteHeader(405)
			return
		}
		if transient > 0 {
			transient--
			w.WriteHeader(503)
			return
		}
		for _, status := range []int{401, 403, 404, 503} {
			if mode == fmt.Sprint(status) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("synthetic-private-response"))
				return
			}
		}
		if mode == "malformed" {
			_, _ = w.Write([]byte(`{"private":"synthetic-private-response"}`))
			return
		}
		if mode == "empty" {
			_, _ = w.Write([]byte(`{"count":0,"next":null,"results":[]}`))
			return
		}
		second := r.URL.RawQuery == "offset=1"
		if mode == "late403" && second {
			w.WriteHeader(403)
			return
		}
		id := int64(9007199254740993)
		if second != reverse {
			id = 1
		}
		if mode == "duplicate" {
			id = 1
		}
		row := map[string]any{"id": id, "status": "synthetic-private-status", "computer": map[string]any{"id": syntheticNoteDevice, "name": "synthetic-excluded-name"}, "blueprint": nil, "reported_at": nil, "log": "synthetic-excluded-log", "edr_status": map[string]string{"status": "synthetic-excluded-edr"}}
		if id == 1 {
			row["blueprint"] = map[string]string{"id": strings.ToUpper(syntheticCategory), "name": "synthetic-excluded-blueprint"}
			row["reported_at"] = ""
		}
		count := 2
		var next any = path + "?offset=1"
		if second {
			next = nil
			if mode == "changedCount" {
				count = 3
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"count": count, "next": next, "results": []any{row}})
	}))
	write(fmt.Sprintf("data \"iru_library_item_statuses\" \"selected\" { library_item_id=%q }\noutput \"status_count\" { value=data.iru_library_item_statuses.selected.record_count }\n", strings.ToUpper(syntheticID)))
	if out := run(0, "apply", "-auto-approve", "-no-color"); strings.Contains(out, "synthetic-private") || strings.Contains(out, syntheticNoteDevice) {
		t.Fatal("sensitive status projection leaked in human output")
	}
	show := run(0, "show", "-json")
	var state struct {
		Values struct {
			RootModule struct {
				Resources []struct {
					Values struct {
						ID            string `json:"id"`
						LibraryItemID string `json:"library_item_id"`
						Count         int    `json:"record_count"`
						Statuses      []struct {
							ID          int64
							DeviceID    string  `json:"device_id"`
							BlueprintID *string `json:"blueprint_id"`
							ReportedAt  *string `json:"reported_at"`
						} `json:"statuses"`
					} `json:"values"`
					Sensitive map[string]any `json:"sensitive_values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if json.Unmarshal([]byte(show), &state) != nil || len(state.Values.RootModule.Resources) != 1 {
		t.Fatal("snapshot state missing")
	}
	item := state.Values.RootModule.Resources[0]
	if item.Values.ID != syntheticID || item.Values.LibraryItemID != strings.ToUpper(syntheticID) || item.Values.Count != 2 || len(item.Values.Statuses) != 2 || item.Sensitive["statuses"] != true {
		t.Fatal("selector, snapshot size or sensitivity mismatch")
	}
	rows := item.Values.Statuses
	if rows[0].ID != 1 || rows[1].ID != 9007199254740993 || rows[0].BlueprintID == nil || *rows[0].BlueprintID != syntheticCategory || rows[0].ReportedAt == nil || *rows[0].ReportedAt != "" || rows[1].BlueprintID != nil || rows[1].ReportedAt != nil || rows[0].DeviceID != syntheticNoteDevice {
		t.Fatal("status snapshot lost integer precision, ordering or optional distinctions")
	}
	if strings.Contains(show, "synthetic-excluded") || !strings.Contains(show, "synthetic-private-status") {
		t.Fatal("excluded payload was persisted or sensitivity incorrectly claimed encryption")
	}
	mu.Lock()
	reverse = true
	mu.Unlock()
	run(0, "plan", "-no-color", "-detailed-exitcode")
	before := run(0, "state", "pull")
	for _, failure := range []string{"401", "403", "404", "503", "malformed", "late403", "duplicate", "changedCount"} {
		mu.Lock()
		mode = failure
		mu.Unlock()
		out := run(1, "plan", "-no-color")
		if strings.Contains(out, "synthetic-private") || strings.Contains(out, "synthetic-excluded") || run(0, "state", "pull") != before {
			t.Fatal("failed status refresh leaked or changed persisted state")
		}
	}
	mu.Lock()
	mode = ""
	transient = 1
	mu.Unlock()
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	mode = "empty"
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	if out := run(0, "show", "-json"); !strings.Contains(out, `"statuses":[]`) || !strings.Contains(out, `"record_count":0`) {
		t.Fatal("empty snapshot was not retained")
	}
	mu.Lock()
	defer mu.Unlock()
	if gets == 0 || writes != 0 {
		t.Fatal("snapshot did not use GET-only observation")
	}
}

func TestTerraformCLILibraryItemStatusesUnknownSelectorDefersAndInvalidRefused(t *testing.T) {
	var gets atomic.Int64
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets.Add(1)
		t.Error("unresolved or invalid selector reached API")
		w.WriteHeader(405)
	}))
	write(fmt.Sprintf("resource \"terraform_data\" \"selector\" { input=%q }\ndata \"iru_library_item_statuses\" \"selected\" { library_item_id=terraform_data.selector.output }", syntheticID))
	if !strings.Contains(run(2, "plan", "-no-color", "-detailed-exitcode"), "read during apply") {
		t.Fatal("unknown selector did not defer")
	}
	for _, id := range []string{"name", "../private", syntheticID + "/status"} {
		write(fmt.Sprintf("data \"iru_library_item_statuses\" \"selected\" { library_item_id=%q }", id))
		run(1, "validate", "-no-color")
	}
	if gets.Load() != 0 {
		t.Fatal("invalid/deferred lookup used API")
	}
}
