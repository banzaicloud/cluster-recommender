// Copyright © 2018 Banzai Cloud
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package recommender

import (
	"github.com/banzaicloud/cloudinfo/pkg/cloudinfo-client/client"
	"github.com/banzaicloud/cloudinfo/pkg/cloudinfo-client/client/attributes"
	"github.com/banzaicloud/cloudinfo/pkg/cloudinfo-client/client/products"
	"github.com/banzaicloud/cloudinfo/pkg/cloudinfo-client/client/regions"
	"github.com/banzaicloud/cloudinfo/pkg/cloudinfo-client/models"
)

// CloudInfoSource declares operations for retrieving information required for the recommender engine
type CloudInfoSource interface {
	// GetAttributeValues retrieves attribute values based on the given arguments
	GetAttributeValues(provider string, service string, region string, attr string) ([]float64, error)

	// GetRegion describes the given region fof the given provider
	GetRegion(provider string, service string, region string) ([]string, error)

	// GetProductDetails retrieves the product details for the provider and region
	GetProductDetails(provider string, service string, region string) ([]*models.ProductDetails, error)
}

// CloudInfoClient application struct to retrieve data for the recommender; wraps the generated product info client
// It implements the CloudInfoSource interface, delegates to the embedded generated client
type CloudInfoClient struct {
	*client.Cloudinfo
}

// NewCloudInfoClient creates a new product info client wrapper instance
func NewCloudInfoClient(pic *client.Cloudinfo) *CloudInfoClient {
	return &CloudInfoClient{Cloudinfo: pic}
}

// GetAttributeValues retrieves available attribute values on the provider in the region for the attribute
func (piCli *CloudInfoClient) GetAttributeValues(provider string, service string, region string, attr string) ([]float64, error) {
	attrParams := attributes.NewGetAttrValuesParams().WithProvider(provider).WithRegion(region).WithAttribute(attr).WithService(service)
	allValues, err := piCli.Attributes.GetAttrValues(attrParams)
	if err != nil {
		return nil, err
	}
	return allValues.Payload.AttributeValues, nil
}

// GetRegion describes the region (eventually returns the zones in the region)
func (piCli *CloudInfoClient) GetRegion(provider string, service string, region string) ([]string, error) {
	grp := regions.NewGetRegionParams().WithProvider(provider).WithService(service).WithRegion(region)
	r, err := piCli.Regions.GetRegion(grp)
	if err != nil {
		return nil, err
	}
	return r.Payload.Zones, nil
}

// GetProductDetails gets the available product details from the provider in the region
func (piCli *CloudInfoClient) GetProductDetails(provider string, service string, region string) ([]*models.ProductDetails, error) {
	gpdp := products.NewGetProductsParams().WithRegion(region).WithProvider(provider).WithService(service)
	allProducts, err := piCli.Products.GetProducts(gpdp)
	if err != nil {
		return nil, err
	}
	return allProducts.Payload.Products, nil
}
