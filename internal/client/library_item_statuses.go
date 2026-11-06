// SPDX-License-Identifier: MPL-2.0
package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"
)

// LibraryItemStatus is an observation, not a desired configuration or a claim
// that an installer ran. Do not retain computer names, logs or EDR details.
type LibraryItemStatus struct {
	ID          int64
	DeviceID    string
	BlueprintID *string
	Status      string
	ReportedAt  *string
}

func (LibraryItemStatus) String() string {
	return "Iru Library Item status (private metadata withheld)"
}
func (LibraryItemStatus) GoString() string {
	return "Iru Library Item status (private metadata withheld)"
}

func decodeLibraryItemStatus(raw json.RawMessage) (LibraryItemStatus, error) {
	var value LibraryItemStatus
	var fields map[string]json.RawMessage
	invalid := errors.New("Iru returned invalid Library Item status metadata (content withheld)")
	if json.Unmarshal(raw, &fields) != nil || json.Unmarshal(fields["id"], &value.ID) != nil || value.ID <= 0 ||
		json.Unmarshal(fields["status"], &value.Status) != nil || strings.TrimSpace(value.Status) == "" || len(value.Status) > 128 || strings.IndexFunc(value.Status, unicode.IsControl) >= 0 {
		return LibraryItemStatus{}, invalid
	}
	var computer struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(fields["computer"], &computer) != nil || !ValidID(computer.ID) {
		return LibraryItemStatus{}, invalid
	}
	value.DeviceID = strings.ToLower(computer.ID)
	if bp := fields["blueprint"]; len(bp) > 0 && string(bp) != "null" {
		var blueprint struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(bp, &blueprint) != nil || !ValidID(blueprint.ID) {
			return LibraryItemStatus{}, invalid
		}
		id := strings.ToLower(blueprint.ID)
		value.BlueprintID = &id
	}
	if reported := fields["reported_at"]; len(reported) > 0 && string(reported) != "null" {
		var timestamp string
		if json.Unmarshal(reported, &timestamp) != nil || len(timestamp) > 128 || strings.IndexFunc(timestamp, unicode.IsControl) >= 0 {
			return LibraryItemStatus{}, invalid
		}
		value.ReportedAt = &timestamp
	}
	return value, nil
}

// ListLibraryItemStatuses uses the documented paginated status endpoint only.
// An empty result is a status snapshot, not proof of item absence, assignment
// placement, device delivery or successful execution. A GET404 remains an error.
func (c *Client) ListLibraryItemStatuses(ctx context.Context, id string) ([]LibraryItemStatus, error) {
	if !ValidID(id) {
		return nil, errors.New("Library Item ID must be a UUID")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	path := "/api/v1/library/library-items/" + strings.ToLower(id) + "/status"
	next := path
	pages, ids := map[string]bool{}, map[int64]bool{}
	items := []LibraryItemStatus{}
	var total paginationTotal
	for page := 0; ; page++ {
		if page >= 100 || pages[next] {
			return nil, errors.New("Library Item statuses exceeded the page bound or repeated a page")
		}
		pages[next] = true
		data, err := c.request(ctx, http.MethodGet, next, nil, http.StatusOK)
		if err != nil {
			return nil, err
		}
		var envelope struct {
			Count   json.RawMessage    `json:"count"`
			Next    json.RawMessage    `json:"next"`
			Results *[]json.RawMessage `json:"results"`
		}
		if json.Unmarshal(data, &envelope) != nil || envelope.Results == nil || len(envelope.Next) == 0 {
			return nil, errors.New("Iru returned an invalid Library Item status page (content withheld)")
		}
		for _, raw := range *envelope.Results {
			value, err := decodeLibraryItemStatus(raw)
			if err != nil {
				return nil, err
			}
			if ids[value.ID] || len(items) >= 10000 {
				return nil, errors.New("Library Item statuses repeated a record ID or exceeded the item bound")
			}
			ids[value.ID] = true
			items = append(items, value)
		}
		if err := total.observe(envelope.Count, len(items), string(envelope.Next) == "null"); err != nil {
			return nil, err
		}
		if string(envelope.Next) == "null" {
			break
		}
		var raw string
		if json.Unmarshal(envelope.Next, &raw) != nil || strings.TrimSpace(raw) == "" {
			return nil, errors.New("Iru returned invalid Library Item status pagination (content withheld)")
		}
		next, err = c.nextLibraryPage(raw, path)
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}
