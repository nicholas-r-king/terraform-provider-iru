// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

func (c *Client) classicAssignmentBlueprint(ctx context.Context, blueprintID, itemID string) error {
	if !ValidID(blueprintID) || !ValidID(itemID) {
		return errors.New("root assignment requires exact Blueprint and Library Item UUIDs")
	}
	bp, err := c.GetBlueprint(ctx, blueprintID)
	if err != nil {
		return err
	}
	if bp.Type != "classic" {
		return errors.New("root assignment currently requires a classic Blueprint; aggregate Library Item reads cannot prove Assignment Map node placement")
	}
	return nil
}

func (c *Client) classicMembership(ctx context.Context, blueprintID, itemID string) (bool, error) {
	_, err := c.FindLibraryItem(ctx, blueprintID, itemID)
	if IsLibraryMembershipAbsent(err) {
		return false, nil
	}
	return err == nil, err
}

// ClassicRootAssignment reads membership only, never the Library Item's content
// or assignment placement in a map. A missing exact parent proves no membership.
func (c *Client) ClassicRootAssignment(ctx context.Context, blueprintID, itemID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := c.classicAssignmentBlueprint(ctx, blueprintID, itemID); err != nil {
		if IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return c.classicMembership(ctx, blueprintID, itemID)
}

// SetClassicRootAssignment returns attempted=true once a POST is attempted,
// including ambiguous failures. It never replays a mutation. The desired state
// is confirmed by a complete read, not inferred from the success UUID array.
func (c *Client) SetClassicRootAssignment(ctx context.Context, blueprintID, itemID string, desired bool) (attempted bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err = c.classicAssignmentBlueprint(ctx, blueprintID, itemID); err != nil {
		if !desired && IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	present, err := c.classicMembership(ctx, blueprintID, itemID)
	if err != nil || present == desired {
		return false, err
	}
	operation := "remove-library-item"
	if desired {
		operation = "assign-library-item"
	}
	endpoint := "/api/v1/blueprints/" + strings.ToLower(blueprintID) + "/" + operation
	data, err := c.request(ctx, http.MethodPost, endpoint, map[string]string{"library_item_id": strings.ToLower(itemID)}, http.StatusOK)
	if err != nil {
		return true, err
	}
	var ids []string
	if json.Unmarshal(data, &ids) != nil || ids == nil || len(ids) > 10000 {
		return true, errors.New("Iru returned invalid assignment confirmation metadata; membership may have changed (content withheld)")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.ToLower(id)
		if !ValidID(id) || seen[id] {
			return true, errors.New("Iru returned ambiguous assignment confirmation metadata; membership may have changed (content withheld)")
		}
		seen[id] = true
	}
	present, err = c.ClassicRootAssignment(ctx, blueprintID, itemID)
	if err != nil {
		return true, err
	}
	if present != desired {
		return true, errors.New("Iru assignment membership did not confirm the requested state; inspect the exact pair before retrying")
	}
	return true, nil
}
