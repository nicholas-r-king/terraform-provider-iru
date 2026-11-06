// SPDX-License-Identifier: MPL-2.0
package client

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// A declared nonzero total contradicts an empty final page. It must not become
// an absence decision or a successful truncated inventory.
func TestDeclaredPaginationTotalCannotProveFalseAbsence(t *testing.T) {
	for _, kind := range []string{"tag", "membership", "inventory"} {
		t.Run(kind, func(t *testing.T) {
			c := testClient(t, func(*http.Request) (*http.Response, error) {
				return response(200, `{"count":1,"results":[],"next":null}`), nil
			})
			var err error
			switch kind {
			case "tag":
				_, err = c.GetTag(context.Background(), testID)
			case "membership":
				_, err = c.FindLibraryItem(context.Background(), testCategory, testID)
			case "inventory":
				_, err = c.ListInventory(context.Background(), "custom_scripts")
			}
			if err == nil || IsTagAbsent(err) || IsLibraryMembershipAbsent(err) {
				t.Fatal("contradictory listing accepted as complete or absent")
			}
		})
	}
}

func TestPaginationTotalsAcrossAllCollectionReaders(t *testing.T) {
	target := `{"id":"` + testID + `","name":"Synthetic"}`
	other := `{"id":"` + testCategory + `","name":"Other"}`
	for _, kind := range []string{"tag", "membership", "inventory"} {
		for _, tc := range []struct {
			name  string
			pages []string
			valid bool
		}{
			{"omitted", []string{`{"results":[` + target + `],"next":null}`}, true},
			{"matching-single", []string{`{"count":1,"results":[` + target + `],"next":null}`}, true},
			{"matching-multiple", []string{`{"count":2,"results":[` + target + `],"next":"?offset=1"}`, `{"count":2,"results":[` + other + `],"next":null}`}, true},
			{"later-omission", []string{`{"count":2,"results":[` + target + `],"next":"?offset=1"}`, `{"results":[` + other + `],"next":null}`}, true},
			{"count-only-later", []string{`{"results":[` + target + `],"next":"?offset=1"}`, `{"count":2,"results":[` + other + `],"next":null}`}, true},
			{"truncated-found", []string{`{"count":2,"results":[` + target + `],"next":null}`}, false},
			{"excess-items", []string{`{"count":0,"results":[` + target + `],"next":null}`}, false},
			{"changing", []string{`{"count":1,"results":[` + target + `],"next":"?offset=1"}`, `{"count":2,"results":[],"next":null}`}, false},
			{"null", []string{`{"count":null,"results":[` + target + `],"next":null}`}, false},
			{"string", []string{`{"count":"synthetic-private","results":[],"next":null}`}, false},
			{"fraction", []string{`{"count":1.5,"results":[],"next":null}`}, false},
			{"negative", []string{`{"count":-1,"results":[],"next":null}`}, false},
			{"over-bound", []string{`{"count":10001,"results":[],"next":null}`}, false},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				calls := 0
				c := testClient(t, func(r *http.Request) (*http.Response, error) {
					if r.Method != http.MethodGet || calls >= len(tc.pages) {
						t.Fatal("unexpected request")
					}
					body := tc.pages[calls]
					calls++
					return response(200, body), nil
				})
				var err error
				switch kind {
				case "tag":
					_, err = c.GetTag(context.Background(), testID)
				case "membership":
					_, err = c.FindLibraryItem(context.Background(), testCategory, testID)
				case "inventory":
					_, err = c.ListInventory(context.Background(), "custom_scripts")
				}
				if tc.valid && (err != nil || calls != len(tc.pages)) {
					t.Fatalf("valid complete listing rejected: %v", err)
				}
				if !tc.valid && (err == nil || IsTagAbsent(err) || IsLibraryMembershipAbsent(err)) {
					t.Fatal("invalid listing accepted or classified as absent")
				}
				if err != nil && strings.Contains(err.Error(), "synthetic-private") {
					t.Fatal("private pagination contents leaked")
				}
			})
		}
	}
}

func TestDeclaredZeroTotalRetainsConfirmedAbsenceSemantics(t *testing.T) {
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		return response(200, `{"count":0,"results":[],"next":null}`), nil
	})
	if _, err := c.GetTag(context.Background(), testID); !IsTagAbsent(err) {
		t.Fatal("validated empty Tag listing did not prove absence")
	}
	if _, err := c.FindLibraryItem(context.Background(), testCategory, testID); !IsLibraryMembershipAbsent(err) {
		t.Fatal("validated empty membership listing did not prove absence")
	}
	if items, err := c.ListInventory(context.Background(), "custom_scripts"); err != nil || len(items) != 0 {
		t.Fatal("validated empty inventory failed")
	}
}
