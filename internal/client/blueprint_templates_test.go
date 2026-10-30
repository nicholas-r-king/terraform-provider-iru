// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestBlueprintTemplateCatalogProjectionAndNumericOrdering(t *testing.T) {
	calls := 0
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != "GET" || req.URL.Path != blueprintTemplatesPath {
			t.Fatal("wrong template catalog request")
		}
		if req.URL.RawQuery == "" {
			return response(200, `{"next":"?offset=1","results":[{"id":2,"name":"Synthetic category","templates":[{"id":16,"name":"Template sixteen","params":{"private":"do-not-retain"}}]}]}`), nil
		}
		return response(200, `{"next":null,"results":[{"id":1,"name":"Second category","templates":[{"id":2,"name":"Template two"}]}]}`), nil
	})
	items, err := c.ListBlueprintTemplates(context.Background())
	if err != nil || calls != 2 || len(items) != 2 || items[0].ID != "2" || items[1].ID != "16" || items[1].CategoryID != "2" {
		t.Fatalf("template catalog failed: %v", err)
	}
	bytes, _ := json.Marshal(items)
	if strings.Contains(string(bytes), "do-not-retain") {
		t.Fatal("template parameters entered projection")
	}
}
func TestBlueprintTemplateInvalidPagesFailClosed(t *testing.T) {
	for _, body := range []string{`{}`, `{"next":null,"results":[{"id":1,"name":"Category"}]}`, `{"next":null,"results":[{"id":1,"name":"Category","templates":[{"id":0,"name":"Invalid"}]}]}`, `{"next":null,"results":[{"id":1,"name":"Category","templates":[{"id":2,"name":"A"},{"id":2,"name":"B"}]}]}`, `{"next":"https://evil.example/api/v1/blueprints/templates/?offset=1","results":[]}`, `{"next":"?offset=1","results":[]}`} {
		calls := 0
		c := testClient(t, func(*http.Request) (*http.Response, error) { calls++; return response(200, body), nil })
		if _, err := c.ListBlueprintTemplates(context.Background()); err == nil || calls > 2 {
			t.Fatal("unsafe or incomplete template page accepted")
		}
	}
}
