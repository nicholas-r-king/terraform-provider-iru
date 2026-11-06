// SPDX-License-Identifier: MPL-2.0
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func statusFixture(id int64) map[string]any {
	return map[string]any{"id": id, "status": "PENDING", "computer": map[string]any{"id": strings.ToUpper(testID), "name": "synthetic-private-device"}, "blueprint": map[string]any{"id": strings.ToUpper(testCategory), "name": "synthetic-private-blueprint"}, "reported_at": nil, "log": "synthetic-private-log", "edr_status": map[string]any{"private": "synthetic-private-edr"}}
}
func statusPage(rows []map[string]any, count int, next any) string {
	data, _ := json.Marshal(map[string]any{"count": count, "next": next, "results": rows})
	return string(data)
}
func TestLibraryItemStatusesCompleteProjectionAndPagination(t *testing.T) {
	path := "/api/v1/library/library-items/" + testID + "/status"
	calls := 0
	c := testClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.Path != path || r.Header.Get("Authorization") != "Bearer "+testToken {
			t.Fatal("status lookup escaped its read-only endpoint")
		}
		if calls == 1 {
			return response(200, statusPage([]map[string]any{statusFixture(9007199254740993)}, 2, "https://synthetic.api.iru.com"+path+"?offset=1")), nil
		}
		if r.URL.RawQuery != "offset=1" || calls != 2 {
			t.Fatal("unexpected pagination request")
		}
		value := statusFixture(2)
		value["blueprint"], value["reported_at"], value["status"] = nil, "", "NEW_UNKNOWN_STATUS"
		return response(200, statusPage([]map[string]any{value}, 2, nil)), nil
	})
	items, err := c.ListLibraryItemStatuses(context.Background(), strings.ToUpper(testID))
	if err != nil || len(items) != 2 || calls != 2 || items[0].ID != 2 || items[1].ID != 9007199254740993 {
		t.Fatal("complete numeric-ID snapshot was not reconstructed", err)
	}
	if items[0].DeviceID != testID || items[0].BlueprintID != nil || items[0].ReportedAt == nil || *items[0].ReportedAt != "" || items[0].Status != "NEW_UNKNOWN_STATUS" || *items[1].BlueprintID != testCategory || items[1].ReportedAt != nil {
		t.Fatal("observation changed UUID spelling, optional distinctions or status meaning")
	}
	data, _ := json.Marshal(items)
	if strings.Contains(string(data), "synthetic-private") || strings.Contains(fmt.Sprintf("%v %#v", items[0], items[0]), testID) {
		t.Fatal("private names/logs/EDR payload or formatted status identity escaped projection")
	}
}
func TestLibraryItemStatusesRejectMalformedRows(t *testing.T) {
	changes := []func(map[string]any){
		func(v map[string]any) { delete(v, "id") },
		func(v map[string]any) { v["id"] = nil },
		func(v map[string]any) { v["id"] = -1 },
		func(v map[string]any) { v["id"] = 1.5 },
		func(v map[string]any) { v["id"] = "synthetic-private-id" },
		func(v map[string]any) { v["status"] = nil },
		func(v map[string]any) { v["status"] = " " },
		func(v map[string]any) { v["status"] = "invalid\nstatus" },
		func(v map[string]any) { v["status"] = strings.Repeat("x", 129) },
		func(v map[string]any) { delete(v, "computer") },
		func(v map[string]any) { v["computer"] = nil },
		func(v map[string]any) { v["computer"] = map[string]any{"id": "synthetic-private-id"} },
		func(v map[string]any) { v["blueprint"] = false },
		func(v map[string]any) { v["blueprint"] = map[string]any{"id": nil} },
		func(v map[string]any) { v["reported_at"] = false },
		func(v map[string]any) { v["reported_at"] = "invalid\nvalue" },
	}
	for i, change := range changes {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			value := statusFixture(1)
			change(value)
			c := testClient(t, func(*http.Request) (*http.Response, error) {
				return response(200, statusPage([]map[string]any{value}, 1, nil)), nil
			})
			items, err := c.ListLibraryItemStatuses(context.Background(), testID)
			if err == nil || items != nil || strings.Contains(err.Error(), "synthetic-private") {
				t.Fatal("malformed response was accepted or leaked")
			}
		})
	}
}
func TestLibraryItemStatusesFailClosedBeforeReturningPartialSnapshot(t *testing.T) {
	path := "/api/v1/library/library-items/" + testID + "/status"
	for _, fault := range []string{"late403", "late404", "late503", "lateMalformed", "duplicate", "countChanged", "truncated", "crossOrigin", "wrongPath", "cycle"} {
		t.Run(fault, func(t *testing.T) {
			calls := 0
			c := testClient(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" || r.URL.Host != "synthetic.api.iru.com" || r.URL.Path != path {
					t.Fatal("unsafe status pagination reached transport")
				}
				if calls == 1 {
					next := path + "?offset=1"
					switch fault {
					case "crossOrigin":
						next = "https://other.invalid" + next
					case "wrongPath":
						next = "/api/v1/devices?offset=1"
					case "truncated":
						return response(200, statusPage([]map[string]any{statusFixture(1)}, 2, nil)), nil
					}
					return response(200, statusPage([]map[string]any{statusFixture(1)}, 2, next)), nil
				}
				if strings.HasPrefix(fault, "late") {
					switch fault {
					case "late403":
						return response(403, "synthetic-private-error"), nil
					case "late404":
						return response(404, "synthetic-private-error"), nil
					case "late503":
						return response(503, "synthetic-private-error"), nil
					default:
						return response(200, `{"private":"synthetic-private-error"}`), nil
					}
				}
				id, count := int64(2), 2
				var next any
				if fault == "duplicate" {
					id = 1
				}
				if fault == "countChanged" {
					count = 3
				}
				if fault == "cycle" {
					next = path + "?offset=1"
				}
				return response(200, statusPage([]map[string]any{statusFixture(id)}, count, next)), nil
			})
			items, err := c.ListLibraryItemStatuses(context.Background(), testID)
			if err == nil || items != nil || calls > 4 || strings.Contains(err.Error(), "synthetic-private") {
				t.Fatal("partial/unsafe snapshot accepted, replay exceeded bound, or error leaked")
			}
		})
	}
}
func TestLibraryItemStatusesEmptyInvalidAndCanceled(t *testing.T) {
	calls := 0
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		calls++
		return response(200, `{"count":0,"results":[],"next":null}`), nil
	})
	for _, id := range []string{"", "name", "../private", testID + "/status"} {
		if _, err := c.ListLibraryItemStatuses(context.Background(), id); err == nil {
			t.Fatal("invalid selector accepted")
		}
	}
	if calls != 0 {
		t.Fatal("invalid selector reached transport")
	}
	items, err := c.ListLibraryItemStatuses(context.Background(), testID)
	if err != nil || items == nil || len(items) != 0 {
		t.Fatal("empty snapshot was not preserved")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.ListLibraryItemStatuses(ctx, testID); err == nil {
		t.Fatal("canceled read succeeded")
	}
}
