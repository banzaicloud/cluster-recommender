// Code generated by go-swagger; DO NOT EDIT.

package models

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	strfmt "github.com/go-openapi/strfmt"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/swag"
)

// NodePool NodePool represents a set of instances with a specific vm type
// swagger:model NodePool
type NodePool struct {

	// Recommended number of nodes in the node pool
	SumNodes int64 `json:"sumNodes,omitempty"`

	// Specifies if the recommended node pool consists of regular or spot/preemptible instance types
	VMClass string `json:"vmClass,omitempty"`

	// vm
	VM *VirtualMachine `json:"vm,omitempty"`
}

// Validate validates this node pool
func (m *NodePool) Validate(formats strfmt.Registry) error {
	var res []error

	if err := m.validateVM(formats); err != nil {
		res = append(res, err)
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}

func (m *NodePool) validateVM(formats strfmt.Registry) error {

	if swag.IsZero(m.VM) { // not required
		return nil
	}

	if m.VM != nil {
		if err := m.VM.Validate(formats); err != nil {
			if ve, ok := err.(*errors.Validation); ok {
				return ve.ValidateName("vm")
			}
			return err
		}
	}

	return nil
}

// MarshalBinary interface implementation
func (m *NodePool) MarshalBinary() ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	return swag.WriteJSON(m)
}

// UnmarshalBinary interface implementation
func (m *NodePool) UnmarshalBinary(b []byte) error {
	var res NodePool
	if err := swag.ReadJSON(b, &res); err != nil {
		return err
	}
	*m = res
	return nil
}
