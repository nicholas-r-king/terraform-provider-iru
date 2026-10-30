// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// DirectoryUser projects only the documented identity/status fields needed to
// select existing device assignments. No deprecated IDs or directory internals.
type DirectoryUser struct {
	ID       string  `json:"id"`
	Name     *string `json:"name"`
	Email    *string `json:"email"`
	Active   *bool   `json:"active"`
	Archived *bool   `json:"archived"`
}

func (DirectoryUser) String() string   { return "Iru directory user (personal metadata withheld)" }
func (DirectoryUser) GoString() string { return "Iru directory user (personal metadata withheld)" }
func (c *Client) GetUser(ctx context.Context, id string) (DirectoryUser, error) {
	if !ValidID(id) {
		return DirectoryUser{}, errors.New("directory user_id must be a UUID")
	}
	data, err := c.request(ctx, http.MethodGet, "/api/v1/users/"+strings.ToLower(id), nil, http.StatusOK)
	if err != nil {
		return DirectoryUser{}, err
	}
	var user DirectoryUser
	if json.Unmarshal(data, &user) != nil || !ValidID(user.ID) || !strings.EqualFold(user.ID, id) {
		return DirectoryUser{}, errors.New("Iru returned invalid directory user metadata (content withheld)")
	}
	user.ID = strings.ToLower(user.ID)
	return user, nil
}
