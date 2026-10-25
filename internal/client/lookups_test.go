// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestBlueprintProjectionAndIdentity(t *testing.T) {
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != "GET" || req.URL.Path != "/api/v1/blueprints/"+testID {
			t.Fatal("unexpected Blueprint request")
		}
		return response(200, `{"id":"`+testID+`","name":"Synthetic blueprint","type":"map","enrollment_code":{"code":"synthetic-enrollment-secret"},"params":{"synthetic":"private"},"computers_count":99}`), nil
	})
	result, err := c.GetBlueprint(context.Background(), strings.ToUpper(testID))
	if err != nil || result.ID != testID || result.Type != "map" {
		t.Fatal("Blueprint metadata lookup failed", err)
	}
	encoded, _ := json.Marshal(result)
	for _, excluded := range []string{"enrollment", "secret", "params", "computers_count"} {
		if strings.Contains(string(encoded), excluded) {
			t.Fatal("Blueprint projection leaked excluded data")
		}
	}
	c = testClient(t, func(*http.Request) (*http.Response, error) {
		return response(200, `{"id":"`+testCategory+`","name":"Synthetic","type":"classic"}`), nil
	})
	if _, err := c.GetBlueprint(context.Background(), testID); err == nil {
		t.Fatal("mismatched Blueprint identity accepted")
	}
}

func TestLibraryPaginationBoundsAndCancellation(t *testing.T) {
	path := "/api/v1/blueprints/" + testCategory + "/list-library-items"
	calls := 0
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		calls++
		return response(200, `{"next":"`+path+`?offset=`+strconv.Itoa(calls)+`","results":[]}`), nil
	})
	if _, err := c.FindLibraryItem(context.Background(), testCategory, testID); err == nil || calls != 100 {
		t.Fatal("listing must stop at its page bound")
	}
	items := make([]LibraryItem, 10001)
	for i := range items {
		items[i] = LibraryItem{ID: fmt.Sprintf("00000000-0000-4000-8000-%012x", i+1), Name: "Synthetic item"}
	}
	encoded, _ := json.Marshal(map[string]any{"next": nil, "results": items})
	c = testClient(t, func(*http.Request) (*http.Response, error) { return response(200, string(encoded)), nil })
	if _, err := c.FindLibraryItem(context.Background(), testCategory, testID); err == nil {
		t.Fatal("listing must enforce its total item bound")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c = testClient(t, func(req *http.Request) (*http.Response, error) {
		cancel()
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	if _, err := c.FindLibraryItem(ctx, testCategory, testID); !errors.Is(err, context.Canceled) {
		t.Fatal("lookup must preserve caller cancellation")
	}
}

func TestLibraryItemLookupPaginatesAndProjects(t *testing.T) {
	path := "/api/v1/blueprints/" + testCategory + "/list-library-items"
	calls := 0
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != "GET" || req.URL.Path != path || req.Header.Get("Authorization") != "Bearer "+testToken {
			t.Fatal("unexpected Library Item request")
		}
		if calls == 1 {
			return response(200, `{"next":"https://synthetic.api.iru.com`+path+`?offset=1","results":[{"id":"`+testCategory+`","name":"Synthetic unrelated"}]}`), nil
		}
		if req.URL.Query().Get("offset") != "1" {
			t.Fatal("pagination query was not followed")
		}
		return response(200, `{"next":null,"results":[{"id":"`+testID+`","name":"Synthetic item","assignment_node_id":"synthetic-node","secret":"synthetic-secret"}]}`), nil
	})
	item, err := c.FindLibraryItem(context.Background(), testCategory, strings.ToUpper(testID))
	if err != nil || calls != 2 || item.ID != testID || item.Name != "Synthetic item" {
		t.Fatal("paginated exact-UUID lookup failed", err)
	}
	data, _ := json.Marshal(item)
	if strings.Contains(string(data), "node") || strings.Contains(string(data), "secret") {
		t.Fatal("membership projection exposed excluded fields")
	}
}

func TestLibraryPaginationRejectsUnsafeURLsAndCycles(t *testing.T) {
	path := "/api/v1/blueprints/" + testCategory + "/list-library-items"
	for _, next := range []string{"https://evil.example/secret", "https://synthetic.api.iru.com/other", "http://synthetic.api.iru.com" + path, "//evil.example" + path, "https://user:synthetic-secret@synthetic.api.iru.com" + path, path + "#fragment", path} {
		calls := 0
		body, _ := json.Marshal(map[string]any{"next": next, "results": []LibraryItem{{ID: testID, Name: "Synthetic item"}}})
		c := testClient(t, func(*http.Request) (*http.Response, error) { calls++; return response(200, string(body)), nil })
		_, err := c.FindLibraryItem(context.Background(), testCategory, testID)
		if err == nil || calls != 1 || strings.Contains(err.Error(), "synthetic-secret") || strings.Contains(err.Error(), "evil.example") {
			t.Fatal("unsafe/repeated pagination must fail before a second request without URL disclosure")
		}
	}
}

func TestLibraryLookupRequiresCompleteUnambiguousPages(t *testing.T) {
	for _, body := range []string{
		`{"next":null,"results":[]}`,
		`{"results":[{"id":"` + testID + `","name":"Synthetic"}]}`,
		`{"next":null,"results":null}`,
		`{"next":null,"results":[{"id":"` + testID + `","name":"Synthetic"},{"id":"` + testID + `","name":"Synthetic duplicate"}]}`,
	} {
		c := testClient(t, func(*http.Request) (*http.Response, error) { return response(200, body), nil })
		if _, err := c.FindLibraryItem(context.Background(), testCategory, testID); err == nil {
			t.Fatal("missing/ambiguous listing accepted")
		}
	}
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid lookup UUID reached transport")
		return nil, nil
	})
	if _, err := c.FindLibraryItem(context.Background(), "../bad", testID); err == nil {
		t.Fatal("invalid Blueprint UUID accepted")
	}
	if _, err := c.GetBlueprint(context.Background(), "name"); err == nil {
		t.Fatal("Blueprint names must not select the first match")
	}
}
