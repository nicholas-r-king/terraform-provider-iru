// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Allowlist metadata only. Never serialize enrollment codes, params or counts.
type Blueprint struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type LibraryItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type libraryMembershipAbsentError struct{}

func (*libraryMembershipAbsentError) Error() string {
	return "Library Item UUID was not found in the selected Blueprint"
}

// IsLibraryMembershipAbsent is true only after a complete validated listing.
// A collection/page HTTP404, permission failure or malformed page is not absence.
func IsLibraryMembershipAbsent(err error) bool {
	var absent *libraryMembershipAbsentError
	return errors.As(err, &absent)
}

func (c *Client) GetBlueprint(ctx context.Context, id string) (Blueprint, error) {
	if !ValidID(id) {
		return Blueprint{}, errors.New("Blueprint ID must be a UUID")
	}
	data, err := c.request(ctx, http.MethodGet, "/api/v1/blueprints/"+strings.ToLower(id), nil, http.StatusOK)
	if err != nil {
		return Blueprint{}, err
	}
	var result Blueprint
	if json.Unmarshal(data, &result) != nil || !ValidID(result.ID) || !strings.EqualFold(id, result.ID) || strings.TrimSpace(result.Name) == "" || strings.TrimSpace(result.Type) == "" {
		return Blueprint{}, errors.New("Iru returned invalid Blueprint metadata (content withheld)")
	}
	result.ID = strings.ToLower(result.ID)
	return result, nil
}

func (c *Client) nextLibraryPage(raw, expectedPath string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Opaque != "" || strings.Contains(raw, "#") {
		return "", errors.New("Iru returned an unsafe pagination URL (withheld)")
	}
	base, _ := url.Parse(c.baseURL)
	if u.IsAbs() {
		if u.Scheme != base.Scheme || !strings.EqualFold(u.Host, base.Host) {
			return "", errors.New("pagination must stay on the configured tenant origin")
		}
	} else if u.Host != "" || u.Scheme != "" {
		return "", errors.New("pagination must stay on the configured tenant origin")
	}
	if u.Path == "" {
		u.Path = expectedPath
	}
	if u.Path != expectedPath || u.EscapedPath() != expectedPath {
		return "", errors.New("pagination must stay on the Library Item listing endpoint")
	}
	if u.RawQuery == "" {
		return expectedPath, nil
	}
	return expectedPath + "?" + u.RawQuery, nil
}

// FindLibraryItem looks up membership by exact UUID, never assignment placement.
// Follow every page and reject duplicate IDs, unsafe URLs, cycles and bounds.
func (c *Client) FindLibraryItem(ctx context.Context, blueprintID, itemID string) (LibraryItem, error) {
	if !ValidID(blueprintID) || !ValidID(itemID) {
		return LibraryItem{}, errors.New("Blueprint and Library Item IDs must be UUIDs")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	path := "/api/v1/blueprints/" + strings.ToLower(blueprintID) + "/list-library-items"
	next := path
	pages, ids := map[string]bool{}, map[string]bool{}
	var found *LibraryItem
	for page := 0; ; page++ {
		if page >= 100 || pages[next] {
			return LibraryItem{}, errors.New("Library Item pagination exceeded its safety bound or repeated a page")
		}
		pages[next] = true
		data, err := c.request(ctx, http.MethodGet, next, nil, http.StatusOK)
		if err != nil {
			return LibraryItem{}, err
		}
		var envelope struct {
			Next    json.RawMessage `json:"next"`
			Results *[]LibraryItem  `json:"results"`
		}
		if json.Unmarshal(data, &envelope) != nil || envelope.Results == nil || len(envelope.Next) == 0 {
			return LibraryItem{}, errors.New("Iru returned an invalid Library Item page (content withheld)")
		}
		for _, item := range *envelope.Results {
			if !ValidID(item.ID) || strings.TrimSpace(item.Name) == "" {
				return LibraryItem{}, errors.New("Iru returned invalid Library Item metadata (content withheld)")
			}
			item.ID = strings.ToLower(item.ID)
			if ids[item.ID] || len(ids) >= 10000 {
				return LibraryItem{}, errors.New("Library Item listing repeated a UUID or exceeded its item safety bound")
			}
			ids[item.ID] = true
			if strings.EqualFold(item.ID, itemID) {
				copy := item
				found = &copy
			}
		}
		if string(envelope.Next) == "null" {
			break
		}
		var raw string
		if json.Unmarshal(envelope.Next, &raw) != nil || strings.TrimSpace(raw) == "" {
			return LibraryItem{}, errors.New("Iru returned invalid Library Item pagination metadata")
		}
		next, err = c.nextLibraryPage(raw, path)
		if err != nil {
			return LibraryItem{}, err
		}
	}
	if found == nil {
		return LibraryItem{}, &libraryMembershipAbsentError{}
	}
	return *found, nil
}
