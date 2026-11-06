// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

func TestTerraformCLIResolvedInstallerSourceConflictStopsBeforeTicket(t *testing.T) {
	for _, kind := range []string{"custom_app", "in_house_app"} {
		t.Run(kind, func(t *testing.T) {
			var writes atomic.Int64
			run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writes.Add(1); w.WriteHeader(503) }))
			content := []byte("Synthetic README-only unit fixture")
			file := filepath.Join(t.TempDir(), "synthetic.pkg")
			if os.WriteFile(file, content, 0600) != nil {
				t.Fatal("fixture")
			}
			fields := `install_type="package"`
			if kind == "in_house_app" {
				fields = `runs_on_iphone=true`
			}
			write(fmt.Sprintf(`resource "terraform_data" "pending" { input="synthetic/existing" }
resource %q "example" {
 name="Synthetic"
 file_key=terraform_data.pending.output
 source_file=%q
 source_sha256=%q
 %s
}`, "iru_"+kind, file, fmt.Sprintf("%x", sha256.Sum256(content)), fields))
			run(2, "plan", "-no-color", "-detailed-exitcode")
			output := run(1, "apply", "-auto-approve", "-no-color")
			if writes.Load() != 0 || !strings.Contains(output, "not both") {
				t.Fatal("resolved conflict reached API or failed for the wrong reason")
			}
		})
	}
}

func TestTerraformCLIUnknownInstallerChecksumDefersUntilResolved(t *testing.T) {
	for _, kind := range []string{"custom_app", "in_house_app"} {
		for _, absent := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/null=%t", kind, absent), func(t *testing.T) {
				var creates, deletes, unexpected atomic.Int64
				fields, endpoint := `install_type="package"`, "/api/v1/library/custom-apps"
				var object any = client.App{ID: syntheticID, AppInput: client.AppInput{Name: "Synthetic", FileKey: "synthetic/existing", InstallType: "package", InstallEnforcement: "install_once"}}
				if kind == "in_house_app" {
					fields, endpoint = `runs_on_iphone=true`, "/api/v1/library/ipa-apps"
					object = client.InHouseApp{ID: syntheticID, InHouseAppInput: client.InHouseAppInput{Name: "Synthetic", FileKey: "synthetic/existing", RunsOnIphone: true}}
				}
				run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if r.Method == "POST" && r.URL.Path == endpoint {
						creates.Add(1)
						w.WriteHeader(201)
					} else if r.Method == "DELETE" && r.URL.Path == endpoint+"/"+syntheticID {
						deletes.Add(1)
						w.WriteHeader(204)
						return
					} else if r.Method != "GET" || r.URL.Path != endpoint+"/"+syntheticID {
						unexpected.Add(1)
						w.WriteHeader(405)
						return
					}
					_ = json.NewEncoder(w).Encode(object)
				}))
				input := fmt.Sprintf("%q", strings.Repeat("a", 64))
				if absent {
					input = "null"
				}
				write(fmt.Sprintf(`resource "terraform_data" "pending" { input=%s }
resource %q "example" {
 name="Synthetic"
 file_key="synthetic/existing"
 source_sha256=terraform_data.pending.output
 %s
}`, input, "iru_"+kind, fields))
				run(2, "plan", "-no-color", "-detailed-exitcode")
				if absent {
					run(0, "apply", "-auto-approve", "-no-color")
					run(0, "plan", "-no-color", "-detailed-exitcode")
					run(0, "destroy", "-auto-approve", "-no-color")
					if creates.Load() != 1 || deletes.Load() != 1 {
						t.Fatal("null checksum changed normal existing-key lifecycle")
					}
				} else {
					out := run(1, "apply", "-auto-approve", "-no-color")
					if creates.Load() != 0 || deletes.Load() != 0 || !strings.Contains(out, "requires source_file") {
						t.Fatal("resolved orphan checksum was not rejected before Iru")
					}
				}
				if unexpected.Load() != 0 {
					t.Fatal("unexpected upload or endpoint")
				}
			})
		}
	}
}
