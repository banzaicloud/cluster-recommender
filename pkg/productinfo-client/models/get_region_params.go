// Code generated by go-swagger; DO NOT EDIT.

package models

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	strfmt "github.com/go-openapi/strfmt"

	"github.com/go-openapi/swag"
)

// GetRegionParams GetRegionParams is a placeholder for the get region route's path parameters
// swagger:model GetRegionParams
type GetRegionParams struct {

	// in:path
	Provider string `json:"provider,omitempty"`

	// in:path
	Region string `json:"region,omitempty"`
}

// Validate validates this get region params
func (m *GetRegionParams) Validate(formats strfmt.Registry) error {
	return nil
}

// MarshalBinary interface implementation
func (m *GetRegionParams) MarshalBinary() ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	return swag.WriteJSON(m)
}

// UnmarshalBinary interface implementation
func (m *GetRegionParams) UnmarshalBinary(b []byte) error {
	var res GetRegionParams
	if err := swag.ReadJSON(b, &res); err != nil {
		return err
	}
	*m = res
	return nil
}
