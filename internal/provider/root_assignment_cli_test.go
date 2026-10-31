// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

const assignmentItemA = "cccccccc-dddd-4eee-8fff-000000000001"
const assignmentItemB = "cccccccc-dddd-4eee-8fff-000000000002"
const assignmentUnrelated = "cccccccc-dddd-4eee-8fff-000000000003"

func TestTerraformCLIClassicRootAssignment(t *testing.T) {
	var mu sync.Mutex
	members := map[string]bool{assignmentUnrelated: true}
	posts := []string{}
	status := 200
	mapType := false
	malformed := false
	denyRemove := false
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		prefix := "/api/v1/blueprints/" + syntheticCategory
		if r.Method == "GET" && r.URL.Path == prefix {
			kind := "classic"
			if mapType {
				kind = "map"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": syntheticCategory, "name": "Synthetic", "type": kind})
			return
		}
		if r.Method == "GET" && r.URL.Path == prefix+"/list-library-items" {
			if status != 200 {
				w.WriteHeader(status)
				return
			}
			items := []map[string]string{}
			for id, present := range members {
				if present {
					items = append(items, map[string]string{"id": id, "name": "Synthetic item"})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": items, "next": nil})
			return
		}
		if r.Method != "POST" {
			t.Error("assignment attempted parent deletion or an undocumented method")
			w.WriteHeader(405)
			return
		}
		var payload map[string]string
		if json.NewDecoder(r.Body).Decode(&payload) != nil || len(payload) != 1 {
			t.Error("unexpected root payload")
			w.WriteHeader(400)
			return
		}
		id := payload["library_item_id"]
		if id != assignmentItemA && id != assignmentItemB {
			t.Error("foreign pair mutated")
			w.WriteHeader(400)
			return
		}
		switch r.URL.Path {
		case prefix + "/assign-library-item":
			members[id] = true
			posts = append(posts, "assign:"+id)
		case prefix + "/remove-library-item":
			posts = append(posts, "remove:"+id)
			if denyRemove {
				w.WriteHeader(403)
				return
			}
			members[id] = false
		default:
			t.Error("invented assignment route")
			w.WriteHeader(400)
			return
		}
		if malformed {
			_, _ = w.Write([]byte(`{"private":"synthetic-private-response"}`))
			return
		}
		_ = json.NewEncoder(w).Encode([]string{id})
	}))
	config := func(bp, item string) {
		write(fmt.Sprintf("resource \"iru_blueprint_library_item_assignment\" \"example\" {\n blueprint_id=%q\n library_item_id=%q\n}\n", bp, item))
	}
	address := "iru_blueprint_library_item_assignment.example"
	config(syntheticCategory, assignmentItemA)
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-detailed-exitcode", "-no-color")
	config(strings.ToUpper(syntheticCategory), strings.ToUpper(assignmentItemA))
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-detailed-exitcode", "-no-color")
	mu.Lock()
	if len(posts) != 1 {
		t.Fatal("UUID case change detached/reassigned membership")
	}
	mu.Unlock()
	run(0, "state", "rm", address)
	config(syntheticCategory, assignmentItemA)
	run(0, "import", "-no-color", address, strings.ToUpper(syntheticCategory+"/"+assignmentItemA))
	run(0, "plan", "-detailed-exitcode", "-no-color")
	before := run(0, "state", "pull")
	for _, code := range []int{401, 403, 404} {
		mu.Lock()
		status = code
		mu.Unlock()
		run(1, "apply", "-refresh-only", "-auto-approve", "-no-color")
		if run(0, "state", "pull") != before {
			t.Fatal("failed collection read erased imported membership")
		}
	}
	mu.Lock()
	status = 200
	mapType = true
	mu.Unlock()
	run(1, "plan", "-no-color")
	run(1, "destroy", "-auto-approve", "-no-color")
	if run(0, "state", "pull") != before {
		t.Fatal("unobservable map changed state")
	}
	mu.Lock()
	mapType = false
	if len(posts) != 1 {
		t.Fatal("import/read/map failure wrote")
	}
	mu.Unlock()
	config(syntheticCategory, assignmentItemB)
	run(2, "plan", "-detailed-exitcode", "-no-color", "-out=replace.plan")
	var plan struct {
		ResourceChanges []struct {
			Type   string `json:"type"`
			Change struct {
				Actions []string `json:"actions"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	if json.Unmarshal([]byte(run(0, "show", "-json", "replace.plan")), &plan) != nil || len(plan.ResourceChanges) != 1 || plan.ResourceChanges[0].Type != "iru_blueprint_library_item_assignment" || strings.Join(plan.ResourceChanges[0].Change.Actions, ",") != "delete,create" {
		t.Fatal("replacement did not isolate membership")
	}
	run(0, "apply", "-auto-approve", "-no-color", "replace.plan")
	mu.Lock()
	if len(posts) != 3 || posts[1] != "remove:"+assignmentItemA || posts[2] != "assign:"+assignmentItemB || !members[assignmentUnrelated] {
		t.Fatal("replacement changed foreign membership")
	}
	members[assignmentItemB] = false
	mu.Unlock()
	run(2, "plan", "-detailed-exitcode", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-detailed-exitcode", "-no-color")
	mu.Lock()
	denyRemove = true
	mu.Unlock()
	before = run(0, "state", "pull")
	run(1, "destroy", "-auto-approve", "-no-color")
	if run(0, "state", "pull") != before {
		t.Fatal("failed detach lost state")
	}
	mu.Lock()
	denyRemove = false
	mu.Unlock()
	run(0, "destroy", "-auto-approve", "-no-color")
	mu.Lock()
	if members[assignmentItemA] || members[assignmentItemB] || !members[assignmentUnrelated] {
		t.Fatal("detach retained target or removed a foreign pair")
	}
	members[assignmentItemB] = true
	beforePosts := len(posts)
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	mu.Lock()
	if len(posts) != beforePosts {
		t.Fatal("adoption replayed assignment POST")
	}
	mu.Unlock()
	run(0, "destroy", "-auto-approve", "-no-color")
	// A success status with a malformed body may have applied remotely. Keep locator.
	mu.Lock()
	malformed = true
	mu.Unlock()
	if out := run(1, "apply", "-auto-approve", "-no-color"); strings.Contains(out, "synthetic-private-response") {
		t.Fatal("private response leaked")
	}
	if !strings.Contains(run(0, "state", "pull"), syntheticCategory+"/"+assignmentItemB) {
		t.Fatal("ambiguous write lost composite identity")
	}
	mu.Lock()
	malformed = false
	mu.Unlock()
	run(0, "state", "rm", address)
	run(0, "import", "-no-color", address, syntheticCategory+"/"+assignmentItemB)
	run(0, "plan", "-detailed-exitcode", "-no-color")
	run(0, "destroy", "-auto-approve", "-no-color")
}

func TestTerraformCLIRootAssignmentRejectsMapAndBadImport(t *testing.T) {
	reads := 0
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads++
		if r.Method != "GET" {
			t.Fatal("map assignment reached mutation")
		}
		_, _ = fmt.Fprintf(w, `{"id":%q,"name":"Synthetic","type":"map"}`, syntheticCategory)
	}))
	write(fmt.Sprintf("resource \"iru_blueprint_library_item_assignment\" \"example\" {\n blueprint_id=%q\n library_item_id=%q\n}\n", syntheticCategory, assignmentItemA))
	write(fmt.Sprintf("resource \"iru_blueprint_library_item_assignment\" \"example\" {\n blueprint_id=%q\n library_item_id=%q\n assignment_node_id=%q\n}\n", syntheticCategory, assignmentItemA, assignmentItemB))
	run(1, "validate", "-no-color")
	if reads != 0 {
		t.Fatal("unsupported node argument reached API")
	}
	write(fmt.Sprintf("resource \"iru_blueprint_library_item_assignment\" \"example\" {\n blueprint_id=%q\n library_item_id=%q\n}\n", syntheticCategory, assignmentItemA))
	for _, id := range []string{"name", syntheticCategory, syntheticCategory + "/bad", syntheticCategory + "/" + assignmentItemA + "/node"} {
		run(1, "import", "-no-color", "iru_blueprint_library_item_assignment.example", id)
	}
	if reads != 0 {
		t.Fatal("invalid composite import reached network")
	}
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "requires a classic Blueprint") {
		t.Fatal("map rejection not explained")
	}
}
