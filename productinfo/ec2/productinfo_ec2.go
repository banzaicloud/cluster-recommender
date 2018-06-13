package ec2

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/pricing"
	"github.com/banzaicloud/telescopes/productinfo"
	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
)

const (
	// Memory represents the memory attribute for the recommender
	Memory = "memory"

	// Cpu represents the cpu attribute for the recommender
	Cpu = "vcpu"
)

// PricingSource list of operations for retrieving pricing information
// Decouples the pricing logic from the aws api
type PricingSource interface {
	GetAttributeValues(input *pricing.GetAttributeValuesInput) (*pricing.GetAttributeValuesOutput, error)
	GetProducts(input *pricing.GetProductsInput) (*pricing.GetProductsOutput, error)
}

// Ec2Infoer encapsulates the data and operations needed to access external resources
type Ec2Infoer struct {
	pricing    PricingSource
	session    *session.Session
	prometheus v1.API
	promQuery  string
}

// NewEc2Infoer creates a new instance of the infoer
func NewEc2Infoer(pricing PricingSource, prom string, pq string) (*Ec2Infoer, error) {
	s, err := session.NewSession()
	if err != nil {
		log.WithError(err).Error("Error creating AWS session")
		return nil, err
	}
	var promApi v1.API
	if prom == "" {
		log.Warn("Prometheus API address is not set, fallback to direct API access.")
		promApi = nil
	} else {
		promClient, err := api.NewClient(api.Config{
			Address: prom,
		})
		if err != nil {
			log.WithError(err).Warn("Error creating Prometheus client, fallback to direct API access.")
			promApi = nil
		} else {
			promApi = v1.NewAPI(promClient)
		}
	}

	return &Ec2Infoer{
		pricing:    pricing,
		session:    s,
		prometheus: promApi,
		promQuery:  pq,
	}, nil
}

// NewPricing creates a new PricingSource with the given configuration
func NewPricing(cfg *aws.Config) PricingSource {

	s, err := session.NewSession(cfg)
	if err != nil {
		log.Fatalf("could not create session. error: [%s]", err.Error())
	}

	pr := pricing.New(s, cfg)
	return pr
}

// NewConfig creates a new  Config instance and returns a pointer to it
func NewConfig() *aws.Config {
	return &aws.Config{Region: aws.String("us-east-1")}
}

// Initialize is not needed on EC2 because price info is changing frequently
func (e *Ec2Infoer) Initialize() (map[string]map[string]productinfo.Price, error) {
	return nil, nil
}

// GetAttributeValues gets the AttributeValues for the given attribute name
// Delegates to the underlying PricingSource instance and unifies (transforms) the response
func (e *Ec2Infoer) GetAttributeValues(attribute string) (productinfo.AttrValues, error) {
	apiValues, err := e.pricing.GetAttributeValues(e.newAttributeValuesInput(attribute))
	if err != nil {
		return nil, err
	}
	var values productinfo.AttrValues
	for _, v := range apiValues.AttributeValues {
		dotValue := strings.Replace(*v.Value, ",", ".", -1)
		floatValue, err := strconv.ParseFloat(strings.Split(dotValue, " ")[0], 64)
		if err != nil {
			log.Warnf("Couldn't parse attribute Value: [%s=%s]: %v", attribute, dotValue, err.Error())
		}
		values = append(values, productinfo.AttrValue{
			Value:    floatValue,
			StrValue: *v.Value,
		})
	}
	log.Debugf("found %s values: %v", attribute, values)
	return values, nil
}

// GetProducts retrieves the available virtual machines based on the arguments provided
// Delegates to the underlying PricingSource instance and performs transformations
func (e *Ec2Infoer) GetProducts(regionId string, attrKey string, attrValue productinfo.AttrValue) ([]productinfo.VmInfo, error) {

	var vms []productinfo.VmInfo
	log.Debugf("Getting available instance types from AWS API. [region=%s, %s=%s]", regionId, attrKey, attrValue.StrValue)

	products, err := e.pricing.GetProducts(e.newGetProductsInput(regionId, attrKey, attrValue))

	if err != nil {
		return nil, err
	}
	for i, price := range products.PriceList {
		pd, err := newPriceData(price)
		if err != nil {
			log.Warn("could not extract pricing info for the item with index: [ %d ]", i)
			continue
		}

		instanceType, err := pd.GetDataForKey("instanceType")
		if err != nil {
			log.Warn("could not retrieve instance type")
			continue
		}
		cpusStr, err := pd.GetDataForKey(Cpu)
		if err != nil {
			log.Warn("could not retrieve vcpu")
			continue
		}
		memStr, err := pd.GetDataForKey(Memory)
		if err != nil {
			log.Warn("could not retrieve memory")
			continue
		}
		gpu, err := pd.GetDataForKey("gpu")
		if err != nil {
			log.Debugf("could not retrieve gpu")
		}
		odPriceStr, err := pd.GetOnDemandPrice()
		if err != nil {
			log.Warn("could not retrieve on demand price")
			continue
		}
		ntwPerf, err := pd.GetDataForKey("networkPerformance")
		if err != nil {
			log.Warn("could not parse network performance")
			continue
		}

		onDemandPrice, _ := strconv.ParseFloat(odPriceStr, 32)
		cpus, _ := strconv.ParseFloat(cpusStr, 32)
		mem, _ := strconv.ParseFloat(strings.Split(memStr, " ")[0], 32)
		gpus, _ := strconv.ParseFloat(gpu, 32)
		vm := productinfo.VmInfo{
			Type:          instanceType,
			OnDemandPrice: onDemandPrice,
			Cpus:          cpus,
			Mem:           mem,
			Gpus:          gpus,
			NtwPerf:       ntwPerf,
		}
		vms = append(vms, vm)
	}
	if vms == nil {
		log.Debugf("couldn't find any virtual machines to recommend")
	}
	log.Debugf("found vms [%s=%s]: %#v", attrKey, attrValue.StrValue, vms)
	return vms, nil
}

type priceData struct {
	awsData aws.JSONValue
	attrMap map[string]interface{}
}

func newPriceData(prData aws.JSONValue) (*priceData, error) {
	pd := priceData{awsData: prData}

	// get the attributes map
	productMap, err := getMapForKey("product", pd.awsData)
	if err != nil {
		return nil, err
	}

	attrMap, err := getMapForKey("attributes", productMap)
	if err != nil {
		return nil, err
	}

	pd.attrMap = attrMap

	return &pd, nil
}
func (pd *priceData) GetDataForKey(attr string) (string, error) {
	if value, ok := pd.attrMap[attr].(string); ok {
		return value, nil
	}
	return "", fmt.Errorf("could not get %s or could not cast %s to string", attr, attr)
}

func (pd *priceData) GetOnDemandPrice() (string, error) {
	termsMap, err := getMapForKey("terms", pd.awsData)
	if err != nil {
		return "", err
	}
	onDemandMap, err := getMapForKey("OnDemand", termsMap)
	if err != nil {
		return "", err
	}
	for _, term := range onDemandMap {
		priceDimensionsMap, err := getMapForKey("priceDimensions", term.(map[string]interface{}))
		if err != nil {
			return "", err
		}
		for _, dimension := range priceDimensionsMap {

			pricePerUnitMap, err := getMapForKey("pricePerUnit", dimension.(map[string]interface{}))
			if err != nil {
				return "", err
			}
			odPrice, ok := pricePerUnitMap["USD"].(string)
			if !ok {
				return "", errors.New("could not get on demand price or could not cast on demand price to string")
			}
			return odPrice, nil
		}
	}
	return "", nil
}

func getMapForKey(key string, srcMap map[string]interface{}) (map[string]interface{}, error) {
	rawMap, ok := srcMap[key]
	if !ok {
		return nil, fmt.Errorf("could not get map for key: [ %s ]", key)
	}

	remap, ok := rawMap.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("the value for key: [ %s ] could not be cast to map[string]interface{}", key)
	}
	return remap, nil
}

// GetRegion gets the api specific region representation based on the provided id
func (e *Ec2Infoer) GetRegion(id string) *endpoints.Region {
	awsp := endpoints.AwsPartition()
	for _, r := range awsp.Regions() {
		if r.ID() == id {
			return &r
		}
	}
	return nil
}

// newAttributeValuesInput assembles a GetAttributeValuesInput instance for querying the provider
func (e *Ec2Infoer) newAttributeValuesInput(attr string) *pricing.GetAttributeValuesInput {
	return &pricing.GetAttributeValuesInput{
		ServiceCode:   aws.String("AmazonEC2"),
		AttributeName: aws.String(attr),
	}
}

// newAttributeValuesInput assembles a GetAttributeValuesInput instance for querying the provider
func (e *Ec2Infoer) newGetProductsInput(regionId string, attrKey string, attrValue productinfo.AttrValue) *pricing.GetProductsInput {
	return &pricing.GetProductsInput{

		ServiceCode: aws.String("AmazonEC2"),
		Filters: []*pricing.Filter{
			{
				Type:  aws.String("TERM_MATCH"),
				Field: aws.String("operatingSystem"),
				Value: aws.String("Linux"),
			},
			{
				Type:  aws.String("TERM_MATCH"),
				Field: aws.String("location"),
				Value: aws.String(e.GetRegion(regionId).Description()),
			},
			{
				Type:  aws.String("TERM_MATCH"),
				Field: aws.String("tenancy"),
				Value: aws.String("shared"),
			},
			{
				Type:  aws.String("TERM_MATCH"),
				Field: aws.String("preInstalledSw"),
				Value: aws.String("NA"),
			},
			{
				Type:  aws.String("TERM_MATCH"),
				Field: aws.String(attrKey),
				Value: aws.String(attrValue.StrValue),
			},
		},
	}
}

// GetRegions returns a map with available regions
// transforms the api representation into a "plain" map
func (e *Ec2Infoer) GetRegions() (map[string]string, error) {
	regionIdMap := make(map[string]string)
	for key, region := range endpoints.AwsPartition().Regions() {
		regionIdMap[key] = region.Description()
	}
	return regionIdMap, nil
}

// GetZones returns the availability zones in a region
func (e *Ec2Infoer) GetZones(region string) ([]string, error) {
	var zones []string
	ec2Svc := ec2.New(e.session, &aws.Config{Region: aws.String(region)})
	azs, err := ec2Svc.DescribeAvailabilityZones(&ec2.DescribeAvailabilityZonesInput{})
	if err != nil {
		return nil, err
	}
	for _, az := range azs.AvailabilityZones {
		if *az.State == "available" {
			zones = append(zones, *az.ZoneName)
		}
	}
	return zones, nil
}

// HasShortLivedPriceInfo - Spot Prices are changing continuously on EC2
func (e *Ec2Infoer) HasShortLivedPriceInfo() bool {
	return true
}

func (e *Ec2Infoer) getSpotPricesFromPrometheus(region string) (map[string]productinfo.SpotPriceInfo, error) {
	log.Debug("getting spot price averages from Prometheus API")
	priceInfo := make(map[string]productinfo.SpotPriceInfo)
	query := fmt.Sprintf(e.promQuery, region)
	log.Debugf("sending prometheus query: %s", query)
	result, err := e.prometheus.Query(context.Background(), query, time.Now())
	if err != nil {
		return nil, err
	} else if result.String() == "" {
		log.Warnf("Prometheus metric is empty")
	} else {
		r := result.(model.Vector)
		for _, value := range r {
			instanceType := string(value.Metric["instance_type"])
			az := string(value.Metric["availability_zone"])
			price, err := strconv.ParseFloat(value.Value.String(), 64)
			if err != nil {
				return nil, err
			}
			if priceInfo[instanceType] == nil {
				priceInfo[instanceType] = make(productinfo.SpotPriceInfo)
			}
			priceInfo[instanceType][az] = price
		}
	}
	return priceInfo, nil
}

func (e *Ec2Infoer) getCurrentSpotPrices(region string) (map[string]productinfo.SpotPriceInfo, error) {
	priceInfo := make(map[string]productinfo.SpotPriceInfo)
	ec2Svc := ec2.New(e.session, &aws.Config{Region: aws.String(region)})
	err := ec2Svc.DescribeSpotPriceHistoryPages(&ec2.DescribeSpotPriceHistoryInput{
		StartTime:           aws.Time(time.Now()),
		ProductDescriptions: []*string{aws.String("Linux/UNIX")},
	}, func(history *ec2.DescribeSpotPriceHistoryOutput, lastPage bool) bool {
		for _, pe := range history.SpotPriceHistory {
			price, err := strconv.ParseFloat(*pe.SpotPrice, 64)
			if err != nil {
				log.WithError(err).Errorf("couldn't parse spot price from history")
				continue
			}
			if priceInfo[*pe.InstanceType] == nil {
				priceInfo[*pe.InstanceType] = make(productinfo.SpotPriceInfo)
			}
			priceInfo[*pe.InstanceType][*pe.AvailabilityZone] = price
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return priceInfo, nil
}

// GetCurrentPrices returns the current spot prices of every instance type in every availability zone in a given region
func (e *Ec2Infoer) GetCurrentPrices(region string) (map[string]productinfo.Price, error) {
	var spotPrices map[string]productinfo.SpotPriceInfo
	var err error
	if e.prometheus != nil {
		spotPrices, err = e.getSpotPricesFromPrometheus(region)
		if err != nil {
			log.WithError(err).Warn("Couldn't get spot price info from Prometheus API, fallback to direct AWS API access.")
		}
	}
	log.Debug("getting current spot prices directly from the AWS API")
	spotPrices, err = e.getCurrentSpotPrices(region)
	if err != nil {
		return nil, err
	}
	prices := make(map[string]productinfo.Price)
	for region, sp := range spotPrices {
		prices[region] = productinfo.Price{
			SpotPrice:     sp,
			OnDemandPrice: -1,
		}
	}
	return prices, nil
}

// GetMemoryAttrName returns the provider representation of the memory attribute
func (e *Ec2Infoer) GetMemoryAttrName() string {
	return Memory
}

// GetCpuAttrName returns the provider representation of the cpu attribute
func (e *Ec2Infoer) GetCpuAttrName() string {
	return Cpu
}

// GetNetworkPerformanceMapper gets the ec2 specific network performance mapper implementation
func (e *Ec2Infoer) GetNetworkPerformanceMapper() (productinfo.NetworkPerfMapper, error) {
	nm := newEc2NetworkMapper()
	return &nm, nil
}
