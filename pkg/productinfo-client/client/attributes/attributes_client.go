// Code generated by go-swagger; DO NOT EDIT.

package attributes

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"github.com/go-openapi/runtime"

	strfmt "github.com/go-openapi/strfmt"
)

// New creates a new attributes API client.
func New(transport runtime.ClientTransport, formats strfmt.Registry) *Client {
	return &Client{transport: transport, formats: formats}
}

/*
Client for attributes API
*/
type Client struct {
	transport runtime.ClientTransport
	formats   strfmt.Registry
}

/*
GetAttributeValues provides a list of available attribute values in a provider s region
*/
func (a *Client) GetAttributeValues(params *GetAttributeValuesParams) (*GetAttributeValuesOK, error) {
	// TODO: Validate the params before sending
	if params == nil {
		params = NewGetAttributeValuesParams()
	}

	result, err := a.transport.Submit(&runtime.ClientOperation{
		ID:                 "getAttributeValues",
		Method:             "GET",
		PathPattern:        "/products/{provider}/{region}/{attribute}",
		ProducesMediaTypes: []string{"application/json"},
		ConsumesMediaTypes: []string{""},
		Schemes:            []string{"http", "https"},
		Params:             params,
		Reader:             &GetAttributeValuesReader{formats: a.formats},
		Context:            params.Context,
		Client:             params.HTTPClient,
	})
	if err != nil {
		return nil, err
	}
	return result.(*GetAttributeValuesOK), nil

}

// SetTransport changes the transport on the client
func (a *Client) SetTransport(transport runtime.ClientTransport) {
	a.transport = transport
}