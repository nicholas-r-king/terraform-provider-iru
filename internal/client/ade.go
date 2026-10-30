// SPDX-License-Identifier: MPL-2.0

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
)

const adePath = "/api/v1/integrations/apple/ade"

type ADEInput struct {
	BlueprintID         string `json:"blueprint_id"`
	UseBlueprintRouting bool   `json:"use_blueprint_routing"`
	Phone               string `json:"phone"`
	Email               string `json:"email"`
}
type ADEIntegration struct {
	ID        string `json:"id"`
	Blueprint struct {
		ID string `json:"id"`
	} `json:"blueprint"`
	Defaults struct {
		Phone *string `json:"phone"`
		Email *string `json:"email"`
	} `json:"defaults"`
	ServerName        string `json:"server_name"`
	AccessTokenExpiry string `json:"access_token_expiry"`
	// Normalized assignment metadata. Legacy responses contain blueprint.id;
	// the current GET description documents blueprint_id and routing explicitly.
	BlueprintID         *string `json:"-"`
	UseBlueprintRouting bool    `json:"-"`
}

func (input ADEInput) Validate() error {
	if input.UseBlueprintRouting {
		if input.BlueprintID != "" {
			return errors.New("ADE Blueprint routing requires blueprint_id to be omitted")
		}
	} else if !ValidID(input.BlueprintID) {
		return errors.New("ADE requires a Blueprint UUID or use_blueprint_routing=true")
	}
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Address != input.Email || strings.TrimSpace(input.Phone) == "" {
		return errors.New("ADE requires an email address and nonblank phone")
	}
	return nil
}

func (input ADEInput) fields() map[string]any {
	var blueprint any
	if !input.UseBlueprintRouting {
		blueprint = input.BlueprintID
	}
	return map[string]any{"blueprint_id": blueprint, "use_blueprint_routing": input.UseBlueprintRouting, "phone": input.Phone, "email": input.Email}
}
func adeMultipart(input ADEInput, token []byte) ([]byte, string, error) {
	if err := input.Validate(); err != nil {
		return nil, "", err
	}
	if len(token) == 0 || len(token) > 1024*1024 {
		return nil, "", errors.New("ADE server token must be nonempty and at most 1 MiB")
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	// A multipart field has no JSON null type. The routing selection is explicit;
	// its absent Blueprint is represented by an empty blueprint_id form field.
	for field, value := range map[string]string{"blueprint_id": input.BlueprintID, "use_blueprint_routing": strconv.FormatBool(input.UseBlueprintRouting), "phone": input.Phone, "email": input.Email} {
		if w.WriteField(field, value) != nil {
			return nil, "", errors.New("could not encode ADE metadata")
		}
	}
	file, err := w.CreateFormFile("file", "server-token.p7m")
	if err != nil {
		return nil, "", errors.New("could not encode ADE file")
	}
	if _, err = file.Write(token); err != nil || w.Close() != nil {
		return nil, "", errors.New("could not complete ADE request (content withheld)")
	}
	return body.Bytes(), w.FormDataContentType(), nil
}
func decodeADE(data []byte, expected string) (ADEIntegration, error) {
	var value ADEIntegration
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || json.Unmarshal(data, &value) != nil || !ValidID(value.ID) || (expected != "" && !strings.EqualFold(value.ID, expected)) || value.Defaults.Email == nil || value.Defaults.Phone == nil {
		return value, errors.New("Iru returned invalid ADE managed fields (content withheld)")
	}
	if err := decodeADEAssignment(fields, &value); err != nil {
		return value, err
	}
	for _, field := range []string{"server_name", "access_token_expiry"} {
		if raw, exists := fields[field]; !exists || string(raw) == "null" {
			return value, errors.New("Iru ADE response is missing metadata (content withheld)")
		}
	}
	value.ID = strings.ToLower(value.ID)
	return value, nil
}

func decodeADEAssignment(fields map[string]json.RawMessage, value *ADEIntegration) error {
	invalid := errors.New("Iru omitted valid or consistent ADE integration Blueprint/routing metadata (content withheld)")
	routeRaw, routeKnown := fields["use_blueprint_routing"]
	if routeKnown && (string(routeRaw) == "null" || json.Unmarshal(routeRaw, &value.UseBlueprintRouting) != nil) {
		return invalid
	}
	var blueprint *string
	if raw, present := fields["blueprint_id"]; present {
		if string(raw) != "null" {
			var id string
			if json.Unmarshal(raw, &id) != nil || !ValidID(id) {
				return invalid
			}
			id = strings.ToLower(id)
			blueprint = &id
		}
		if blueprint == nil && (!routeKnown || !value.UseBlueprintRouting) {
			return invalid
		}
	} else if value.UseBlueprintRouting {
		// The GET description explicitly pairs routing=true with blueprint_id=null.
		// An omitted field or null legacy object is not proof of that assignment.
		return invalid
	}
	if value.Blueprint.ID != "" {
		if !ValidID(value.Blueprint.ID) || value.UseBlueprintRouting || (blueprint != nil && !strings.EqualFold(*blueprint, value.Blueprint.ID)) {
			return invalid
		}
		id := strings.ToLower(value.Blueprint.ID)
		blueprint = &id
	}
	if !value.UseBlueprintRouting && blueprint == nil {
		return invalid
	}
	if value.UseBlueprintRouting && blueprint != nil {
		return invalid
	}
	value.BlueprintID = blueprint
	value.Blueprint.ID = ""
	if blueprint != nil {
		value.Blueprint.ID = *blueprint
	}
	return nil
}
func (c *Client) CreateADEIntegration(ctx context.Context, input ADEInput, token []byte) (ADEIntegration, error) {
	body, contentType, err := adeMultipart(input, token)
	if err != nil {
		return ADEIntegration{}, err
	}
	// Iru documents HTTP 200 for ADE integration creation.
	data, err := c.requestEncoded(ctx, http.MethodPost, adePath+"/", body, contentType, http.StatusOK)
	if err != nil {
		return ADEIntegration{}, err
	}
	return decodeADE(data, "")
}
func (c *Client) GetADEIntegration(ctx context.Context, id string) (ADEIntegration, error) {
	if !ValidID(id) {
		return ADEIntegration{}, errors.New("ADE integration ID must be a UUID")
	}
	data, err := c.request(ctx, http.MethodGet, adePath+"/"+strings.ToLower(id), nil, http.StatusOK)
	if err != nil {
		return ADEIntegration{}, err
	}
	return decodeADE(data, id)
}
func (c *Client) UpdateADEIntegration(ctx context.Context, id string, input ADEInput) (ADEIntegration, error) {
	if !ValidID(id) {
		return ADEIntegration{}, errors.New("ADE integration ID must be a UUID")
	}
	if err := input.Validate(); err != nil {
		return ADEIntegration{}, err
	}
	data, err := c.request(ctx, http.MethodPatch, adePath+"/"+strings.ToLower(id), input.fields(), http.StatusOK)
	if err != nil {
		return ADEIntegration{}, err
	}
	return decodeADE(data, id)
}
func (c *Client) RenewADEIntegration(ctx context.Context, id string, input ADEInput, token []byte) (ADEIntegration, error) {
	if !ValidID(id) {
		return ADEIntegration{}, errors.New("ADE integration ID must be a UUID")
	}
	body, contentType, err := adeMultipart(input, token)
	if err != nil {
		return ADEIntegration{}, err
	}
	data, err := c.requestEncoded(ctx, http.MethodPost, adePath+"/"+strings.ToLower(id)+"/renew", body, contentType, http.StatusOK)
	if err != nil {
		return ADEIntegration{}, err
	}
	return decodeADE(data, id)
}
func (c *Client) DeleteADEIntegration(ctx context.Context, id string) error {
	if !ValidID(id) {
		return errors.New("ADE integration ID must be a UUID")
	}
	_, err := c.request(ctx, http.MethodDelete, adePath+"/"+strings.ToLower(id), nil, http.StatusNoContent)
	if IsNotFound(err) {
		return nil
	}
	return err
}
