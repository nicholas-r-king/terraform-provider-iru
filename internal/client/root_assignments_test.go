// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestClassicRootAssignmentWireAndIdempotence(t *testing.T) {
	present := false
	posts := []string{}
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet && req.URL.Path == "/api/v1/blueprints/"+testCategory {
			return response(200, `{"id":"`+testCategory+`","name":"Synthetic","type":"classic"}`), nil
		}
		if req.Method == http.MethodGet && req.URL.Path == "/api/v1/blueprints/"+testCategory+"/list-library-items" {
			items := []LibraryItem{}
			if present {
				items = append(items, LibraryItem{ID: testID, Name: "Synthetic item"})
			}
			b, _ := json.Marshal(map[string]any{"results": items, "next": nil})
			return response(200, string(b)), nil
		}
		if req.Method != http.MethodPost {
			t.Fatal("root assignment sent an undocumented method")
		}
		var body map[string]string
		if json.NewDecoder(req.Body).Decode(&body) != nil || len(body) != 1 || body["library_item_id"] != testID {
			t.Fatal("root write sent node/foreign metadata")
		}
		switch req.URL.Path {
		case "/api/v1/blueprints/" + testCategory + "/assign-library-item":
			present = true
		case "/api/v1/blueprints/" + testCategory + "/remove-library-item":
			present = false
		default:
			t.Fatal("invented assignment route")
		}
		posts = append(posts, req.URL.Path)
		return response(200, `["`+testID+`"]`), nil
	})
	ctx := context.Background()
	for _, desired := range []bool{true, true, false, false} {
		attempted, err := c.SetClassicRootAssignment(ctx, strings.ToUpper(testCategory), strings.ToUpper(testID), desired)
		if err != nil {
			t.Fatal(err)
		}
		if len(posts) == 0 || present != desired {
			t.Fatal("membership not verified")
		}
		_ = attempted
	}
	if len(posts) != 2 || !strings.HasSuffix(posts[1], "/remove-library-item") {
		t.Fatal("idempotence replayed mutations")
	}
}

func TestRootAssignmentRejectsMapsAndInvalidIDsBeforeWriting(t *testing.T) {
	for _, kind := range []string{"map", "unexpected"} {
		calls := 0
		c := testClient(t, func(req *http.Request) (*http.Response, error) {
			calls++
			if req.Method != "GET" {
				t.Fatal("unsupported Blueprint reached write")
			}
			return response(200, `{"id":"`+testCategory+`","name":"Synthetic","type":"`+kind+`"}`), nil
		})
		if _, err := c.ClassicRootAssignment(context.Background(), testCategory, testID); err == nil {
			t.Fatal("unobservable map placement accepted")
		}
		if attempted, err := c.SetClassicRootAssignment(context.Background(), testCategory, testID, true); err == nil || attempted || calls != 2 {
			t.Fatal("unsupported map changed membership")
		}
	}
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid identity reached transport")
		return nil, nil
	})
	for _, pair := range [][2]string{{"../bad", testID}, {testCategory, "name"}} {
		if _, err := c.ClassicRootAssignment(context.Background(), pair[0], pair[1]); err == nil {
			t.Fatal("invalid read identity")
		}
		if attempted, err := c.SetClassicRootAssignment(context.Background(), pair[0], pair[1], true); err == nil || attempted {
			t.Fatal("invalid write identity")
		}
	}
}

func TestRootAssignmentCollectionFailureIsNotAbsence(t *testing.T) {
	for _, status := range []int{401, 403, 404} {
		c := testClient(t, func(req *http.Request) (*http.Response, error) {
			if req.Method != "GET" {
				t.Fatal("failed read sent mutation")
			}
			if strings.HasSuffix(req.URL.Path, "list-library-items") {
				return response(status, `{"private":"synthetic-private-response"}`), nil
			}
			return response(200, `{"id":"`+testCategory+`","name":"Synthetic","type":"classic"}`), nil
		})
		if _, err := c.ClassicRootAssignment(context.Background(), testCategory, testID); err == nil || strings.Contains(err.Error(), "synthetic-private") {
			t.Fatal("collection failure became absence or leaked response")
		}
		if attempted, err := c.SetClassicRootAssignment(context.Background(), testCategory, testID, false); err == nil || attempted {
			t.Fatal("collection failure triggered remove")
		}
	}
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != "GET" {
			t.Fatal("absent parent reached mutation")
		}
		return response(404, `{}`), nil
	})
	if present, err := c.ClassicRootAssignment(context.Background(), testCategory, testID); err != nil || present {
		t.Fatal("exact absent parent did not prove absence")
	}
	if attempted, err := c.SetClassicRootAssignment(context.Background(), testCategory, testID, false); err != nil || attempted {
		t.Fatal("removing absent parent assignment wrote")
	}
	if attempted, err := c.SetClassicRootAssignment(context.Background(), testCategory, testID, true); err == nil || attempted {
		t.Fatal("attempted assign into missing parent")
	}
}

func TestRootAssignmentMutationAmbiguityNeverReplays(t *testing.T) {
	for _, failure := range []string{"403", "429", "503", "transport", "malformed", "null", "duplicate", "not-converged", "read-after-write"} {
		t.Run(failure, func(t *testing.T) {
			posts := 0
			c := testClient(t, func(req *http.Request) (*http.Response, error) {
				if req.Method == "GET" {
					if strings.HasSuffix(req.URL.Path, "list-library-items") {
						return response(200, `{"results":[],"next":null}`), nil
					}
					if failure == "read-after-write" && posts > 0 {
						return response(403, `{"private":"synthetic-private"}`), nil
					}
					return response(200, `{"id":"`+testCategory+`","name":"Synthetic","type":"classic"}`), nil
				}
				if req.Method != "POST" {
					t.Fatal("unexpected method")
				}
				posts++
				switch failure {
				case "403":
					return response(403, `{}`), nil
				case "429":
					return response(429, `{}`), nil
				case "503":
					return response(503, `{}`), nil
				case "transport":
					return nil, errors.New("synthetic-private")
				case "malformed":
					return response(200, `{"private":"synthetic-private"}`), nil
				case "null":
					return response(200, `null`), nil
				case "duplicate":
					return response(200, `["`+testID+`","`+strings.ToUpper(testID)+`"]`), nil
				default:
					return response(200, `["`+testID+`"]`), nil
				}
			})
			attempted, err := c.SetClassicRootAssignment(context.Background(), testCategory, testID, true)
			if !attempted || err == nil || posts != 1 || strings.Contains(err.Error(), "synthetic-private") {
				t.Fatal("ambiguous mutation replayed, lost locator or leaked response")
			}
		})
	}
}
