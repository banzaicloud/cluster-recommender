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
	"github.com/banzaicloud/cloudinfo/pkg/cloudinfo-client/client/attribute"
	"github.com/banzaicloud/cloudinfo/pkg/cloudinfo-client/client/products"
	"github.com/banzaicloud/cloudinfo/pkg/cloudinfo-client/client/provider"
	"github.com/banzaicloud/cloudinfo/pkg/cloudinfo-client/client/region"
	"github.com/banzaicloud/cloudinfo/pkg/cloudinfo-client/client/service"
	"github.com/banzaicloud/cloudinfo/pkg/cloudinfo-client/models"
	"github.com/go-openapi/runtime"
	"github.com/goph/emperror"
)

// CloudInfoSource declares operations for retrieving information required for the recommender engine
type CloudInfoSource interface {
	// GetAttributeValues retrieves attribute values based on the given arguments
	GetAttributeValues(provider string, service string, region string, attr string) ([]float64, error)

	// GetZones describes the given region fof the given provider
	GetZones(provider string, service string, region string) ([]string, error)

	// GetProductDetails retrieves the product details for the provider and region
	GetProductDetails(provider string, service string, region string) ([]*models.ProductDetails, error)
}

// CloudInfoClient application struct to retrieve data for the recommender; wraps the generated product info client
// It implements the CloudInfoSource interface, delegates to the embedded generated client
type CloudInfoClient struct {
	*client.Cloudinfo
}

const (
	cloudInfoErrTag    = "cloud-info"
	cloudInfoCliErrTag = "cloud-info-client"
)

// NewCloudInfoClient creates a new product info client wrapper instance
func NewCloudInfoClient(pic *client.Cloudinfo) *CloudInfoClient {
	return &CloudInfoClient{Cloudinfo: pic}
}

// GetAttributeValues retrieves available attribute values on the provider in the region for the attribute
func (ciCli *CloudInfoClient) GetAttributeValues(provider string, service string, region string, attr string) ([]float64, error) {
	attrParams := attribute.NewGetAttrValuesParams().WithProvider(provider).WithRegion(region).WithAttribute(attr).WithService(service)

	allValues, err := ciCli.Attribute.GetAttrValues(attrParams)
	if err != nil {
		return nil, discriminateErrCtx(err)
	}
	return allValues.Payload.AttributeValues, nil
}

// GetZones describes the region (eventually returns the zones in the region)
func (ciCli *CloudInfoClient) GetZones(prv string, svc string, reg string) ([]string, error) {
	grp := region.NewGetRegionParams().WithProvider(prv).WithService(svc).WithRegion(reg)

	r, err := ciCli.Region.GetRegion(grp)
	if err != nil {
		return nil, discriminateErrCtx(err)
	}
	return r.Payload.Zones, nil
}

// GetProductDetails gets the available product details from the provider in the region
func (ciCli *CloudInfoClient) GetProductDetails(provider string, service string, region string) ([]*models.ProductDetails, error) {
	gpdp := products.NewGetProductsParams().WithRegion(region).WithProvider(provider).WithService(service)

	allProducts, err := ciCli.Products.GetProducts(gpdp)
	if err != nil {
		return nil, discriminateErrCtx(err)
	}
	return allProducts.Payload.Products, nil
}

// GetProvider validates provider
func (ciCli *CloudInfoClient) GetProvider(prv string) (string, error) {
	gpp := provider.NewGetProviderParams().WithProvider(prv)

	provider, err := ciCli.Provider.GetProvider(gpp)
	if err != nil {
		return "", discriminateErrCtx(err)
	}

	return provider.Payload.Provider.Provider, nil
}

// GetService validates service
func (ciCli *CloudInfoClient) GetService(prv string, svc string) (string, error) {
	gsp := service.NewGetServiceParams().WithProvider(prv).WithService(svc)

	provider, err := ciCli.Service.GetService(gsp)
	if err != nil {
		return "", discriminateErrCtx(err)
	}

	return provider.Payload.Service.Service, nil
}

// GetRegion validates region
func (ciCli *CloudInfoClient) GetRegion(prv, svc, reg string) (string, error) {
	grp := region.NewGetRegionParams().WithProvider(prv).WithService(svc).WithRegion(reg)

	r, err := ciCli.Region.GetRegion(grp)
	if err != nil {
		return "", discriminateErrCtx(err)
	}

	return r.Payload.Name, nil
}

func discriminateErrCtx(err error) error {

	if _, ok := err.(*runtime.APIError); ok {
		// the service can be reached
		return emperror.With(err, cloudInfoErrTag)
	}
	// handle other cloud info errors here

	// probably connectivity error (should it be analized further?!)
	return emperror.With(err, cloudInfoCliErrTag)
}
