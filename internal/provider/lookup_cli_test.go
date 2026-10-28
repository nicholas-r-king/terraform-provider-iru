// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestTerraformCLILookups(t *testing.T) {
	var mu sync.Mutex
	name, present := "Synthetic item", true
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if req.Method != "GET" || req.Header.Get("Authorization") != "Bearer "+cliToken {
			t.Error("lookup attempted a mutation or lacked synthetic authentication")
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/api/v1/blueprints/" + syntheticCategory:
			_, _ = fmt.Fprintf(w, `{"id":%q,"name":"Synthetic blueprint","type":"map","enrollment_code":{"code":"synthetic-enrollment-secret"}}`, syntheticCategory)
		case "/api/v1/blueprints/" + syntheticCategory + "/list-library-items":
			if !present {
				_, _ = fmt.Fprint(w, `{"next":null,"results":[]}`)
			} else if req.URL.RawQuery == "" {
				_, _ = fmt.Fprintf(w, `{"next":"?offset=1","results":[{"id":%q,"name":"Synthetic unrelated"}]}`, syntheticCategory)
			} else {
				_, _ = fmt.Fprintf(w, `{"next":null,"results":[{"id":%q,"name":%q}]}`, syntheticID, name)
			}
		default:
			t.Error("lookup reached an unexpected endpoint")
			w.WriteHeader(404)
		}
	}))
	write(fmt.Sprintf("data \"iru_blueprint\" \"selected\" {\n blueprint_id = %q\n}\ndata \"iru_library_item\" \"selected\" {\n blueprint_id = data.iru_blueprint.selected.id\n library_item_id = %q\n}\noutput \"item_name\" { value = data.iru_library_item.selected.name }\n", strings.ToUpper(syntheticCategory), strings.ToUpper(syntheticID)))
	run(0, "validate", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	state := run(0, "show", "-json")
	if strings.Contains(state, "enrollment") || strings.Contains(state, "secret") {
		t.Fatal("Blueprint enrollment secret entered Terraform state")
	}
	mu.Lock()
	name = "Synthetic renamed item"
	mu.Unlock()
	run(2, "plan", "-no-color", "-detailed-exitcode")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	present = false
	mu.Unlock()
	run(1, "plan", "-no-color")
}
