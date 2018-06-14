package productinfo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
	log "github.com/sirupsen/logrus"
)

func (v AttrValues) floatValues() []float64 {
	floatValues := make([]float64, len(v))
	for _, av := range v {
		floatValues = append(floatValues, av.Value)
	}
	return floatValues
}

// SpotPriceInfo represents different prices per availability zones
type SpotPriceInfo map[string]float64

// Price describes the on demand price and spot prices per availability zones
type Price struct {
	OnDemandPrice float64       `json:"onDemandPrice"`
	SpotPrice     SpotPriceInfo `json:"spotPrice"`
}

// VmInfo representation of a virtual machine
type VmInfo struct {
	Type          string        `json:"type"`
	OnDemandPrice float64       `json:"onDemandPrice"`
	SpotPrice     SpotPriceInfo `json:"spotPrice"`
	Cpus          float64       `json:"cpusPerVm"`
	Mem           float64       `json:"memPerVm"`
	Gpus          float64       `json:"gpusPerVm"`
	NtwPerf       string        `json:"ntwPerf"`
}

// IsBurst returns true if the EC2 instance vCPU is burst type
// the decision is made based on the instance type
func (vm VmInfo) IsBurst() bool {
	return strings.HasPrefix(strings.ToUpper(vm.Type), "T")
}

//NetworkPerformance returns the network performance category for the vm
func (vm VmInfo) NetworkPerformance(nm NetworkPerfMapper) string {
	nc, err := nm.MapNetworkPerf(vm)
	if err != nil {
		log.Warnf("could not get network performance for vm [%s], error: [%s]", vm.Type, err.Error())
	}
	return nc
}

// NewCachingProductInfo creates a new CachingProductInfo instance
func NewCachingProductInfo(ri time.Duration, cache *cache.Cache, infoers map[string]ProductInfoer) (*CachingProductInfo, error) {
	if infoers == nil || cache == nil {
		return nil, errors.New("could not create product infoer")
	}

	pi := CachingProductInfo{
		productInfoers:  infoers,
		vmAttrStore:     cache,
		renewalInterval: ri,
	}

	// todo add validator here
	return &pi, nil
}

// Start starts the information retrieval in a new goroutine
func (pi *CachingProductInfo) Start(ctx context.Context) {

	renew := func() {
		var providerWg sync.WaitGroup
		for provider, infoer := range pi.productInfoers {
			providerWg.Add(1)
			go func(p string, i ProductInfoer) {
				defer providerWg.Done()
				log.Infof("renewing %s product info", p)
				_, err := pi.Initialize(p)
				if err != nil {
					log.Errorf("couldn't renew attribute values in cache: %s", err.Error())
					return
				}
				attributes := []string{Cpu, Memory}
				for _, attr := range attributes {
					attrValues, err := pi.renewAttrValues(p, attr)
					if err != nil {
						log.Errorf("couldn't renew attribute values in cache: %s", err.Error())
						return
					}
					regions, err := i.GetRegions()
					if err != nil {
						log.Errorf("couldn't renew attribute values in cache: %s", err.Error())
						return
					}
					for regionId := range regions {
						for _, v := range attrValues {
							_, err := pi.renewVmsWithAttr(p, regionId, attr, v)
							if err != nil {
								log.Errorf("couldn't renew attribute values in cache: %s", err.Error())
							}
						}
					}
				}
			}(provider, infoer)
		}
		providerWg.Wait()
		log.Info("finished renewing product info")
	}

	renewShortLived := func() {
		var providerWg sync.WaitGroup
		for provider, infoer := range pi.productInfoers {
			providerWg.Add(1)
			go func(p string, i ProductInfoer) {
				defer providerWg.Done()
				if i.HasShortLivedPriceInfo() {
					log.Infof("renewing short lived %s product info", p)
					var wg sync.WaitGroup
					regions, err := i.GetRegions()
					if err != nil {
						log.Errorf("couldn't renew attribute values in cache: %s", err.Error())
						return
					}
					for regionId := range regions {
						wg.Add(1)
						go func(p string, r string) {
							defer wg.Done()
							_, err := pi.renewShortLivedInfo(p, r)
							if err != nil {
								log.Errorf("couldn't renew short lived info in cache: %s", err.Error())
								return
							}
						}(p, regionId)
					}

					wg.Wait()
				}
			}(provider, infoer)
		}
		providerWg.Wait()
		log.Info("finished renewing short lived product info")
	}

	go renew()
	ticker := time.NewTicker(pi.renewalInterval)
	go func() {
		for {
			select {
			case <-ticker.C:
				renew()
			case <-ctx.Done():
				log.Debugf("closing ticker")
				ticker.Stop()
				return
			}
		}
	}()
	go renewShortLived()
	shortTicker := time.NewTicker(1 * time.Minute)
	for {
		select {
		case <-shortTicker.C:
			renewShortLived()
		case <-ctx.Done():
			log.Debugf("closing ticker")
			shortTicker.Stop()
			return
		}
	}
}

// Initialize stores the result of the Infoer's Initialize output in cache
func (pi *CachingProductInfo) Initialize(provider string) (map[string]map[string]Price, error) {
	allPrices, err := pi.productInfoers[provider].Initialize()
	if err != nil {
		return nil, err
	}
	for region, ap := range allPrices {
		for instType, p := range ap {
			pi.vmAttrStore.Set(pi.getPriceKey(provider, region, instType), p, pi.renewalInterval)
		}
	}
	return allPrices, nil
}

// GetAttrValues returns a slice with the values for the given attribute name
func (pi *CachingProductInfo) GetAttrValues(provider string, attribute string) ([]float64, error) {
	v, err := pi.getAttrValues(provider, attribute)
	if err != nil {
		return nil, err
	}
	floatValues := v.floatValues()
	log.Debugf("%s attribute values: %v", attribute, floatValues)
	return floatValues, nil
}

func (pi *CachingProductInfo) getAttrValues(provider string, attribute string) (AttrValues, error) {
	attrCacheKey := pi.getAttrKey(provider, attribute)
	if cachedVal, ok := pi.vmAttrStore.Get(attrCacheKey); ok {
		log.Debugf("Getting available %s values from cache.", attribute)
		return cachedVal.(AttrValues), nil
	}
	values, err := pi.renewAttrValues(provider, attribute)
	if err != nil {
		return nil, err
	}
	return values, nil
}

func (pi *CachingProductInfo) getAttrKey(provider string, attribute string) string {
	return fmt.Sprintf(AttrKeyTemplate, provider, attribute)
}

// renewAttrValues retrieves attribute values from the cloud provider and refreshes the attribute store with them
func (pi *CachingProductInfo) renewAttrValues(provider string, attribute string) (AttrValues, error) {
	attr, err := pi.toProviderAttribute(provider, attribute)
	if err != nil {
		return nil, err
	}
	values, err := pi.productInfoers[provider].GetAttributeValues(attr)
	if err != nil {
		return nil, err
	}
	pi.vmAttrStore.Set(pi.getAttrKey(provider, attribute), values, pi.renewalInterval)
	return values, nil
}

// HasShortLivedPriceInfo signals if a product info provider has frequently changing price info
func (pi *CachingProductInfo) HasShortLivedPriceInfo(provider string) bool {
	return pi.productInfoers[provider].HasShortLivedPriceInfo()
}

// GetPrice returns the ondemand price and zone averaged computed spot price for a given instance type in a given region
func (pi *CachingProductInfo) GetPrice(provider string, region string, instanceType string, zones []string) (float64, float64, error) {
	var p Price
	if cachedVal, ok := pi.vmAttrStore.Get(pi.getPriceKey(provider, region, instanceType)); ok {
		log.Debugf("Getting price info from cache [provider=%s, region=%s, type=%s].", provider, region, instanceType)
		p = cachedVal.(Price)
	} else {
		allPriceInfo, err := pi.renewShortLivedInfo(provider, region)
		if err != nil {
			return 0, 0, err
		}
		p = allPriceInfo[instanceType]
	}
	var sumPrice float64
	for _, z := range zones {
		for zone, price := range p.SpotPrice {
			if zone == z {
				sumPrice += price
			}
		}
	}
	return p.OnDemandPrice, sumPrice / float64(len(zones)), nil
}

func (pi *CachingProductInfo) getPriceKey(provider string, region string, instanceType string) string {
	return fmt.Sprintf(PriceKeyTemplate, provider, region, instanceType)
}

// renewAttrValues retrieves attribute values from the cloud provider and refreshes the attribute store with them
func (pi *CachingProductInfo) renewShortLivedInfo(provider string, region string) (map[string]Price, error) {
	prices, err := pi.productInfoers[provider].GetCurrentPrices(region)
	if err != nil {
		return nil, err
	}
	for instType, p := range prices {
		pi.vmAttrStore.Set(pi.getPriceKey(provider, region, instType), p, 2*time.Minute)
	}
	return prices, nil
}

func (pi *CachingProductInfo) toProviderAttribute(provider string, attr string) (string, error) {
	switch attr {
	case Cpu:
		return pi.productInfoers[provider].GetCpuAttrName(), nil
	case Memory:
		return pi.productInfoers[provider].GetMemoryAttrName(), nil
	}
	return "", fmt.Errorf("unsupported attribute: %s", attr)
}

// GetVmsWithAttrValue returns a slice with the virtual machines for the given region, attribute and value
func (pi *CachingProductInfo) GetVmsWithAttrValue(provider string, regionId string, attrKey string, value float64) ([]VmInfo, error) {

	log.Debugf("Getting instance types and on demand prices. [regionId=%s, %s=%v]", regionId, attrKey, value)
	vmCacheKey := pi.getVmKey(provider, regionId, attrKey, value)
	if cachedVal, ok := pi.vmAttrStore.Get(vmCacheKey); ok {
		log.Debugf("Getting available instance types from cache. [regionId=%s, %s=%v]", regionId, attrKey, value)
		return cachedVal.([]VmInfo), nil
	}
	attrValue, err := pi.getAttrValue(provider, attrKey, value)
	if err != nil {
		return nil, err
	}
	vms, err := pi.renewVmsWithAttr(provider, regionId, attrKey, *attrValue)
	if err != nil {
		return nil, err
	}
	return vms, nil
}

func (pi *CachingProductInfo) getVmKey(provider string, region string, attrKey string, attrValue float64) string {
	return fmt.Sprintf(VmKeyTemplate, provider, region, attrKey, attrValue)
}

func (pi *CachingProductInfo) renewVmsWithAttr(provider string, regionId string, attrKey string, attrValue AttrValue) ([]VmInfo, error) {
	attr, err := pi.toProviderAttribute(provider, attrKey)
	if err != nil {
		return nil, err
	}
	values, err := pi.productInfoers[provider].GetProducts(regionId, attr, attrValue)
	if err != nil {
		return nil, err
	}
	pi.vmAttrStore.Set(pi.getVmKey(provider, regionId, attrKey, attrValue.Value), values, pi.renewalInterval)
	return values, nil
}

func (pi *CachingProductInfo) getAttrValue(provider string, attrKey string, attrValue float64) (*AttrValue, error) {
	attrValues, err := pi.getAttrValues(provider, attrKey)
	if err != nil {
		return nil, err
	}
	for _, av := range attrValues {
		if av.Value == attrValue {
			return &av, nil
		}
	}
	return nil, errors.New("couldn't find attribute Value")
}

// GetZones returns the availability zones in a region
func (pi *CachingProductInfo) GetZones(provider string, region string) ([]string, error) {
	zoneCacheKey := pi.getZonesKey(provider, region)

	// check the cache
	if cachedVal, ok := pi.vmAttrStore.Get(zoneCacheKey); ok {
		log.Debugf("Getting available zones from cache. [provider=%s, region=%s]", provider, region)
		return cachedVal.([]string), nil
	}

	// retrieve zones from the provider
	zones, err := pi.productInfoers[provider].GetZones(region)
	if err != nil {
		log.Errorf("error while retrieving zones. provider: %s, region: %s", provider, region)
		return nil, err
	}

	// cache the results / use the cahce default expiry
	pi.vmAttrStore.Set(zoneCacheKey, zones, 0)
	return zones, nil
}

func (pi *CachingProductInfo) getZonesKey(provider string, region string) string {
	return fmt.Sprintf(ZoneKeyTemplate, provider, region)
}

// GetNetworkPerfMapper returns the provider specific network performance mapper
func (pi *CachingProductInfo) GetNetworkPerfMapper(provider string) (NetworkPerfMapper, error) {
	if infoer, ok := pi.productInfoers[provider]; ok {
		return infoer.GetNetworkPerformanceMapper() // this also can return with err!
	}
	return nil, fmt.Errorf("could not retrieve network perf mapper for provider: [%s]", provider)
}

// GetRegions gets the regions for the provided provider
func (pi *CachingProductInfo) GetRegions(provider string) (map[string]string, error) {
	regionCacheKey := pi.getRegionsKey(provider)

	// check the cache
	if cachedVal, ok := pi.vmAttrStore.Get(regionCacheKey); ok {
		log.Debugf("Getting available regions from cache. [provider=%s]", provider)
		return cachedVal.(map[string]string), nil
	}

	// retrieve zones from the provider
	regions, err := pi.productInfoers[provider].GetRegions()
	if err != nil {
		log.Errorf("could not retrieve regions. provider: %s", provider)
		return nil, err
	}

	// cache the results / use the cahce default expiry
	pi.vmAttrStore.Set(regionCacheKey, regions, 0)
	return regions, nil
}

func (pi *CachingProductInfo) getRegionsKey(provider string) string {
	return fmt.Sprintf(RegionKeyTemplate, provider)
}
