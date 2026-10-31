// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

func TestTerraformCLITag(t *testing.T) {
	var object *client.Tag
	var mu sync.Mutex
	posts, deletes := 0, 0
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if req.Header.Get("Authorization") != "Bearer "+cliToken || (req.URL.Path != "/api/v1/tags" && req.URL.Path != "/api/v1/tags/"+syntheticID) {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "GET":
			if req.URL.Path != "/api/v1/tags" {
				t.Error("Tag refresh invented a detail endpoint")
				w.WriteHeader(404)
				return
			}
			items := []client.Tag{}
			if object != nil {
				items = append(items, *object)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"next": nil, "results": items})
			return
		case "POST", "PATCH":
			var input struct {
				Name string `json:"name"`
			}
			if json.NewDecoder(req.Body).Decode(&input) != nil || strings.TrimSpace(input.Name) == "" {
				w.WriteHeader(400)
				return
			}
			if req.Method == "PATCH" && object == nil {
				w.WriteHeader(404)
				return
			}
			object = &client.Tag{ID: syntheticID, Name: input.Name}
			if req.Method == "POST" {
				posts++
				w.WriteHeader(201)
			}
			_ = json.NewEncoder(w).Encode(object)
		case "DELETE":
			deletes++
			object = nil
			w.WriteHeader(204)
		default:
			w.WriteHeader(405)
		}
	}))
	config := func(name string) { write(fmt.Sprintf("resource \"iru_tag\" \"example\" {\n name = %q\n}\n", name)) }
	config("Synthetic tag")
	run(0, "validate", "-no-color")
	run(2, "plan", "-no-color", "-detailed-exitcode")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	run(0, "state", "rm", "iru_tag.example")
	run(0, "import", "-no-color", "iru_tag.example", strings.ToUpper(syntheticID))
	run(0, "plan", "-no-color", "-detailed-exitcode")
	config("Synthetic renamed")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	object.Name = "Synthetic external drift"
	mu.Unlock()
	run(2, "plan", "-no-color", "-detailed-exitcode")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "destroy", "-auto-approve", "-no-color")
	mu.Lock()
	if posts != 1 || deletes != 1 {
		t.Error("import/update created duplicates or delete was not sent")
	}
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	mu.Lock()
	object = nil
	mu.Unlock()
	run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
	if strings.TrimSpace(run(0, "state", "list")) != "" {
		t.Fatal("missing Tag did not leave Terraform state")
	}
}

func TestTerraformCLIBlueprint(t *testing.T) {
	var object *client.ManagedBlueprint
	var mu sync.Mutex
	posts, patches, deletes := 0, 0, 0
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if req.Header.Get("Authorization") != "Bearer "+cliToken || (req.URL.Path != "/api/v1/blueprints" && req.URL.Path != "/api/v1/blueprints/"+syntheticID) {
			w.WriteHeader(404)
			return
		}
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
			if req.ParseForm() != nil || req.Form.Get("enrollment_code.is_active") != "false" || req.Form.Has("color") || req.Form.Has("icon") {
				t.Error("unexpected Blueprint form or activation")
				w.WriteHeader(400)
				return
			}
			if req.Method == "POST" {
				posts++
				object = &client.ManagedBlueprint{ID: syntheticID, Type: req.Form.Get("type"), Color: "aqua-800", Icon: "ss-files"}
				code, active := "000123", false
				object.EnrollmentCode.Code, object.EnrollmentCode.Active = &code, &active
			} else {
				patches++
				if req.Form.Has("type") {
					t.Error("sent immutable type on update")
				}
			}
			object.Name, object.Description = req.Form.Get("name"), req.Form.Get("description")
			if code := req.Form.Get("enrollment_code.code"); code != "" {
				object.EnrollmentCode.Code = &code
			}
			if req.Method == "POST" {
				w.WriteHeader(201)
			} else {
				_ = json.NewEncoder(w).Encode(map[string]string{"id": syntheticID})
				return
			}
		case "DELETE":
			deletes++
			object = nil
			w.WriteHeader(204)
			return
		default:
			w.WriteHeader(405)
			return
		}
		_ = json.NewEncoder(w).Encode(object)
	}))
	config := func(name string) {
		write(fmt.Sprintf("resource \"iru_blueprint\" \"example\" {\n name = %q\n type = \"map\"\n}\n", name))
	}
	config("Synthetic blueprint")
	run(0, "validate", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	run(0, "state", "rm", "iru_blueprint.example")
	run(0, "import", "-no-color", "iru_blueprint.example", strings.ToUpper(syntheticID))
	run(0, "plan", "-no-color", "-detailed-exitcode")
	config("Synthetic renamed")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	object.Name = "Synthetic external drift"
	mu.Unlock()
	run(2, "plan", "-no-color", "-detailed-exitcode")
	run(0, "apply", "-auto-approve", "-no-color")
	if output := run(1, "plan", "-destroy", "-no-color"); !strings.Contains(output, "Blueprint deletion blocked") {
		t.Fatal("destroy plan did not explain device unmanagement")
	}
	if output := run(1, "destroy", "-auto-approve", "-no-color"); !strings.Contains(output, "Blueprint deletion blocked") {
		t.Fatal("destroy was not refused")
	}
	if !strings.Contains(run(0, "state", "list"), "iru_blueprint.example") {
		t.Fatal("blocked deletion lost state")
	}
	for _, change := range []string{`type = "classic"`, `type = "map"
 source_type = "template"
 source_id = "1"`} {
		write("resource \"iru_blueprint\" \"example\" {\n name = \"Synthetic renamed\"\n" + change + "\n}\n")
		if output := run(1, "plan", "-no-color"); !strings.Contains(output, "Blueprint replacement blocked") {
			t.Fatal("destructive replacement not rejected")
		}
	}
	config("Synthetic renamed")
	mu.Lock()
	if posts != 1 || patches != 2 || deletes != 0 {
		t.Errorf("unexpected lifecycle counts: %d/%d/%d", posts, patches, deletes)
	}
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	mu.Lock()
	object = nil
	mu.Unlock()
	run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
	if strings.TrimSpace(run(0, "state", "list")) != "" {
		t.Fatal("Blueprint 404 did not remove state")
	}
	write("resource \"iru_blueprint\" \"bad\" {\n name = \"Synthetic bad\"\n type = \"map\"\n color = \"orange-500\"\n}\n")
	run(1, "validate", "-no-color")
}
