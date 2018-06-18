package productinfo

import (
	"context"
	"time"

	"github.com/patrickmn/go-cache"
)

const (
	// Memory represents the memory attribute for the recommender
	Memory = "memory"

	// Cpu represents the cpu attribute for the recommender
	Cpu = "cpu"

	// VmKeyTemplate format for generating vm cache keys
	VmKeyTemplate = "/banzaicloud.com/recommender/%s/%s/vms"

	// AttrKeyTemplate format for generating attribute cache keys
	AttrKeyTemplate = "/banzaicloud.com/recommender/%s/attrValues/%s"

	// PriceKeyTemplate format for generating price cache keys
	PriceKeyTemplate = "/banzaicloud.com/recommender/%s/%s/prices/%s"

	// ZoneKeyTemplate format for generating zone cache keys
	ZoneKeyTemplate = "/banzaicloud.com/recommender/%s/%s/zones/"

	// RegionKeyTemplate format for generating region cache keys
	RegionKeyTemplate = "/banzaicloud.com/recommender/%s/regions/"
)

// ProductInfoer gathers operations for retrieving cloud provider information for recommendations
// it also decouples provider api specific code from the recommender
type ProductInfoer interface {
	// Initialize is called once per product info renewals so it can be used to download a large price descriptor
	Initialize() (map[string]map[string]Price, error)

	// GetAttributeValues gets the attribute values for the given attribute from the external system
	GetAttributeValues(attribute string) (AttrValues, error)

	// GetProducts gets product information based on the given arguments from an external system
	GetProducts(regionId string) ([]VmInfo, error)

	// GetZones returns the availability zones in a region
	GetZones(region string) ([]string, error)

	// GetRegions retrieves the available regions form the external system
	GetRegions() (map[string]string, error)

	// HasShortLivedPriceInfo signals if a product info provider has frequently changing price info
	HasShortLivedPriceInfo() bool

	// GetCurrentPrices retrieves all the spot prices in a region
	GetCurrentPrices(region string) (map[string]Price, error)

	// GetMemoryAttrName returns the provider representation of the memory attribute
	GetMemoryAttrName() string

	// GetCpuAttrName returns the provider representation of the cpu attribute
	GetCpuAttrName() string

	// GetNetworkPerformanceMapper returns the provider specific network performance mapper
	GetNetworkPerformanceMapper() (NetworkPerfMapper, error)
}

// ProductInfo is the main entry point for retrieving vm type characteristics and pricing information on different cloud providers
type ProductInfo interface {
	// Start starts the product information retrieval in a new goroutine
	Start(ctx context.Context)

	// Initialize is called once per product info renewals so it can be used to download a large price descriptor
	Initialize(provider string) (map[string]map[string]Price, error)

	// GetAttrValues returns a slice with the possible values for a given attribute on a specific provider
	GetAttrValues(provider string, attribute string) ([]float64, error)

	// GetVmsWithAttrValue returns a slice with all those virtual machines in a region that have the required value for a given attribute
	GetVmsWithAttrValue(provider string, regionId string, attrKey string, value float64) ([]VmInfo, error)

	// GetZones returns all the availability zones for a region
	GetZones(provider string, region string) ([]string, error)

	// HasShortLivedPriceInfo signals if a product info provider has frequently changing price info
	HasShortLivedPriceInfo(provider string) bool

	// GetPrice returns the on demand price and the zone averaged computed spot price for a given instance type in a given region
	GetPrice(provider string, region string, instanceType string, zones []string) (float64, float64, error)

	// GetNetworkPerfMapper retrieves the network performance mapper implementation
	GetNetworkPerfMapper(provider string) (NetworkPerfMapper, error)
}

// CachingProductInfo is the module struct, holds configuration and cache
// It's the entry point for the product info retrieval and management subsystem
type CachingProductInfo struct {
	productInfoers  map[string]ProductInfoer `validate:"required"`
	renewalInterval time.Duration
	vmAttrStore     *cache.Cache
}

// AttrValue represents an attribute value
type AttrValue struct {
	StrValue string
	Value    float64
}

// AttrValues a slice of AttrValues
type AttrValues []AttrValue

var (
	// telescope supported network performance of vm-s

	// NTW_LOW the low network performance category
	NTW_LOW = "low"
	// NTW_MEDIUM the medium network performance category
	NTW_MEDIUM = "medium"
	// NTW_HIGH the high network performance category
	NTW_HIGH = "high"
	// NTW_EXTRA the highest network performance category
	NTW_EXTRA = "extra"
)

// NetworkPerfMapper operations related  to mapping between virtual machines to network performance categories
type NetworkPerfMapper interface {
	// MapNetworkPerf gets the network performance category for the given
	MapNetworkPerf(vm VmInfo) (string, error)
}
