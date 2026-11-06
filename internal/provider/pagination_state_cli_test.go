// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestTerraformCLIIncompleteCollectionsPreserveState(t *testing.T) {
	for _, kind := range []string{"tag", "blueprint_library_item_assignment"} {
		t.Run(kind, func(t *testing.T) {
			var mu sync.Mutex
			broken := ""
			writes := 0
			run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if r.Method != http.MethodGet {
					writes++
					w.WriteHeader(405)
					return
				}
				if r.URL.Path == "/api/v1/blueprints/"+syntheticCategory {
					fmt.Fprintf(w, `{"id":%q,"name":"Synthetic","type":"classic"}`, syntheticCategory)
					return
				}
				endpoint := "/api/v1/tags"
				if kind != "tag" {
					endpoint = "/api/v1/blueprints/" + syntheticCategory + "/list-library-items"
				}
				if r.URL.Path != endpoint {
					t.Error("unexpected fixture path")
					w.WriteHeader(400)
					return
				}
				item := fmt.Sprintf(`{"id":%q,"name":"Synthetic"}`, syntheticID)
				switch broken {
				case "truncated-absent":
					fmt.Fprint(w, `{"count":1,"results":[],"next":null}`)
					return
				case "truncated-found":
					fmt.Fprintf(w, `{"count":2,"results":[%s],"next":null}`, item)
					return
				}
				if broken != "" {
					if r.URL.RawQuery == "" {
						fmt.Fprintf(w, `{"count":1,"results":[%s],"next":"?offset=1"}`, item)
						return
					}
					switch broken {
					case "late-403":
						w.WriteHeader(403)
					case "late-404":
						w.WriteHeader(404)
					case "late-503":
						w.Header().Set("Retry-After", "0")
						w.WriteHeader(503)
					case "late-malformed":
						fmt.Fprint(w, `{"private":"synthetic-private-response"}`)
					case "changed-total":
						fmt.Fprint(w, `{"count":2,"results":[],"next":null}`)
					case "duplicate":
						fmt.Fprintf(w, `{"count":1,"results":[%s],"next":null}`, item)
					default:
						t.Error("unknown fault")
						w.WriteHeader(400)
					}
					return
				}
				fmt.Fprintf(w, `{"count":1,"results":[%s],"next":null}`, item)
			}))
			address := "iru_" + kind + ".example"
			config := "name=\"Synthetic\""
			importID := syntheticID
			if kind != "tag" {
				config = fmt.Sprintf("blueprint_id=%q\nlibrary_item_id=%q", syntheticCategory, syntheticID)
				importID = syntheticCategory + "/" + syntheticID
			}
			write(fmt.Sprintf("resource %q \"example\" {\n%s\n}\n", "iru_"+kind, config))
			run(0, "import", "-no-color", address, importID)
			before := run(0, "state", "pull")
			for _, fault := range []string{"truncated-absent", "truncated-found", "late-403", "late-404", "late-503", "late-malformed", "changed-total", "duplicate"} {
				mu.Lock()
				broken = fault
				mu.Unlock()
				out := run(1, "apply", "-refresh-only", "-auto-approve", "-no-color")
				if strings.Contains(out, "synthetic-private-response") {
					t.Fatal("private response leaked")
				}
				if run(0, "state", "pull") != before {
					t.Fatalf("%s changed persisted state", fault)
				}
			}
			mu.Lock()
			broken = ""
			if writes != 0 {
				t.Fatal("read failure attempted a write")
			}
			mu.Unlock()
			run(0, "plan", "-detailed-exitcode", "-no-color")
		})
	}
}
