package recommender

import (
	"errors"
	"fmt"
	"math"
	"sort"

	log "github.com/sirupsen/logrus"
)

const (
	Memory = "memory"
	Cpu    = "cpu"
)

type Engine struct {
	VmRegistries map[string]VmRegistry
}

func NewEngine(vmRegistries map[string]VmRegistry) (*Engine, error) {
	if vmRegistries == nil {
		return nil, errors.New("could not create engine")
	}
	return &Engine{
		VmRegistries: vmRegistries,
	}, nil
}

// swagger:parameters recommendClusterSetup
type ClusterRecommendationReq struct {
	// Total number of CPUs requested for the cluster
	SumCpu float64 `json:"sumCpu"`
	// Total memory requested for the cluster (GB)
	SumMem float64 `json:"sumMem"`
	// Minimum number of nodes in the recommended cluster
	MinNodes int `json:"minNodes,omitempty"`
	// Maximum number of nodes in the recommended cluster
	MaxNodes int `json:"maxNodes,omitempty"`
	// If true, recommended instance types will have a similar size
	SameSize bool `json:"sameSize,omitempty"`
	// Percentage of regular (on-demand) nodes in the recommended cluster
	OnDemandPct int `json:"onDemandPct,omitempty"`
	// Availability zones that the cluster should expand to
	Zones []string `json:"zones,omitempty"`
	// Total number of GPUs requested for the cluster
	SumGpu int `json:"sumGpu,omitempty"`
}

// A ValidationError is an error that is used when the required input fails validation.
// swagger:response recommendationResp
type ClusterRecommendationResp struct {
	// The cloud provider
	Provider string `json:"provider"`
	// Availability zones in the recommendation - a multi-zone recommendation means that all node pools should expand to all zones
	Zones []string `json:"zones,omitempty"`
	// Recommended node pools
	NodePools []NodePool `json:"nodePools"`
}

// Represents a set of instances with a specific vm type
type NodePool struct {
	// Recommended virtual machine type
	VmType VirtualMachine `json:"vm""`
	// Recommended number of nodes in the node pool
	SumNodes int `json:"sumNodes"`
	// Specifies if the recommended node pool consists of regular or spot/preemptible instance types
	VmClass string `json:"vmClass"`
}

// Description of an instance type
type VirtualMachine struct {
	// Instance type
	Type string `json:"type"`
	// Average price of the instance (differs from on demand price in case of spot or preemptible instances)
	AvgPrice float64 `json:"avgPrice"`
	// Regular price of the instance type
	OnDemandPrice float64 `json:"onDemandPrice"`
	// Number of CPUs in the instance type
	Cpus float64 `json:"cpusPerVm"`
	// Available memory in the instance type (GB)
	Mem float64 `json:"memPerVm"`
	// Number of GPUs in the instance type
	Gpus float64 `json:"gpusPerVm"`
}

func (v *VirtualMachine) getAttrValue(attr string) float64 {
	switch attr {
	case Cpu:
		return v.Cpus
	case Memory:
		return v.Mem
	default:
		return 0
	}
}

type vmFilter func(vm VirtualMachine, req ClusterRecommendationReq) bool

func (e *Engine) minMemRatioFilter(vm VirtualMachine, req ClusterRecommendationReq) bool {
	minMemToCpuRatio := req.SumMem / req.SumCpu
	if vm.Mem/vm.Cpus < minMemToCpuRatio {
		return false
	}
	return true
}

func (e *Engine) minCpuRatioFilter(vm VirtualMachine, req ClusterRecommendationReq) bool {
	minCpuToMemRatio := req.SumCpu / req.SumMem
	if vm.Cpus/vm.Mem < minCpuToMemRatio {
		return false
	}
	return true
}

// TODO: i/o filter, nw filter, gpu filter, etc...

type VmRegistry interface {
	getAvailableAttributeValues(attr string) ([]float64, error)
	findVmsWithAttrValues(region string, zones []string, attr string, values []float64) ([]VirtualMachine, error)
}

type ByAvgPricePerCpu []VirtualMachine

func (a ByAvgPricePerCpu) Len() int      { return len(a) }
func (a ByAvgPricePerCpu) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByAvgPricePerCpu) Less(i, j int) bool {
	pricePerCpu1 := a[i].AvgPrice / a[i].Cpus
	pricePerCpu2 := a[j].AvgPrice / a[j].Cpus
	return pricePerCpu1 < pricePerCpu2
}

type ByAvgPricePerMemory []VirtualMachine

func (a ByAvgPricePerMemory) Len() int      { return len(a) }
func (a ByAvgPricePerMemory) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByAvgPricePerMemory) Less(i, j int) bool {
	pricePerMem1 := a[i].AvgPrice / a[i].Mem
	pricePerMem2 := a[j].AvgPrice / a[j].Mem
	return pricePerMem1 < pricePerMem2
}

func (e *Engine) RecommendCluster(provider string, region string, req ClusterRecommendationReq) (*ClusterRecommendationResp, error) {
	log.Infof("recommending cluster configuration")
	attributes := []string{Cpu, Memory}
	nodePools := make(map[string][]NodePool, 2)
	for _, attr := range attributes {
		var sum float64
		var vmFilters []vmFilter
		switch attr {
		case Memory:
			sum = req.SumMem
			vmFilters = []vmFilter{e.minCpuRatioFilter}
		case Cpu:
			sum = req.SumCpu
			vmFilters = []vmFilter{e.minMemRatioFilter}
		default:
			return nil, fmt.Errorf("unsupported attribute: %s", attr)
		}

		maxValuePerVm := sum / float64(req.MinNodes)
		minValuePerVm := sum / float64(req.MaxNodes)

		vmRegistry := e.VmRegistries[provider]

		allValues, err := vmRegistry.getAvailableAttributeValues(attr)
		if err != nil {
			return nil, err
		}

		values, err := e.findValuesBetween(allValues, minValuePerVm, maxValuePerVm)
		if err != nil {
			return nil, err
		}

		vmsInRange, err := vmRegistry.findVmsWithAttrValues(region, req.Zones, attr, values)
		if err != nil {
			return nil, err
		}

		var filteredVms []VirtualMachine

		for _, vm := range vmsInRange {
			for _, filter := range vmFilters {
				if filter(vm, req) {
					filteredVms = append(filteredVms, vm)
				}
			}
		}

		if len(filteredVms) == 0 {
			return nil, errors.New("couldn't find any VMs to recommend")
		}

		var nps []NodePool

		// find cheapest onDemand instance from the list - based on pricePer attribute
		selectedOnDemand := filteredVms[0]
		for _, vm := range filteredVms {
			if vm.OnDemandPrice/vm.getAttrValue(attr) < selectedOnDemand.OnDemandPrice/selectedOnDemand.getAttrValue(attr) {
				selectedOnDemand = vm
			}
		}

		var sumOnDemandValue = sum * float64(req.OnDemandPct) / 100
		var sumSpotValue = sum - sumOnDemandValue

		// create and append on-demand pool
		onDemandPool := NodePool{
			SumNodes: int(math.Ceil(sumOnDemandValue / selectedOnDemand.getAttrValue(attr))),
			VmClass:  "regular",
			VmType:   selectedOnDemand,
		}

		nps = append(nps, onDemandPool)

		// sort and cut
		switch attr {
		case Memory:
			sort.Sort(ByAvgPricePerMemory(filteredVms))
		case Cpu:
			sort.Sort(ByAvgPricePerCpu(filteredVms))
		default:
			return nil, fmt.Errorf("unsupported attribute: %s", attr)
		}

		N := int(math.Min(float64(findN(values, sum)), float64(len(filteredVms))))
		M := int(math.Min(math.Ceil(float64(N)*1.5), float64(len(filteredVms))))

		recommendedVms := filteredVms[:M]

		// create spot nodepools
		for _, vm := range recommendedVms {
			nps = append(nps, NodePool{
				SumNodes: 0,
				VmClass:  "spot",
				VmType:   vm,
			})
		}

		// fill up instances in spot pools
		i := 0
		var sumValueInPools float64 = 0
		for sumValueInPools < sumSpotValue {
			nodePoolIdx := i%N + 1
			if nodePoolIdx == 1 {
				// always add a new instance to the cheapest option and move on
				nps[nodePoolIdx].SumNodes += 1
				sumValueInPools += nps[nodePoolIdx].VmType.getAttrValue(attr)
				i++
			} else if float64(nps[nodePoolIdx].SumNodes+1)*nps[nodePoolIdx].VmType.getAttrValue(attr) > float64(nps[1].SumNodes)*nps[1].VmType.getAttrValue(attr) {
				// for other pools, if adding another vm would exceed the current sum of the cheapest option, move on to the next one
				i++
			} else {
				// otherwise add a new one, but do not move on to the next one
				nps[nodePoolIdx].SumNodes += 1
				sumValueInPools += nps[nodePoolIdx].VmType.getAttrValue(attr)
			}
		}
		log.Infof("recommeded node pools by %s: %#v", attr, nps)
		nodePools[attr] = nps
	}

	return &ClusterRecommendationResp{
		Provider:  "aws",
		Zones:     req.Zones,
		NodePools: e.findCheapestNodePoolSet(nodePools),
	}, nil
}

func (e *Engine) findCheapestNodePoolSet(nodePoolSets map[string][]NodePool) []NodePool {
	var cheapestNpSet []NodePool
	var bestPrice float64
	for attr, nodePools := range nodePoolSets {
		var sumPrice float64
		var sumCpus float64
		var sumMem float64
		for _, np := range nodePools {
			if np.VmClass == "regular" {
				sumPrice += float64(np.SumNodes) * np.VmType.OnDemandPrice
			} else {
				sumPrice += float64(np.SumNodes) * np.VmType.AvgPrice
			}
			sumCpus += float64(np.SumNodes) * np.VmType.Cpus
			sumMem += float64(np.SumNodes) * np.VmType.Mem
		}
		log.Debugf("sum cpus [%s]: %v", attr, sumCpus)
		log.Debugf("sum mem [%s]: %v", attr, sumMem)
		log.Debugf("sum price [%s]: %v", attr, sumPrice)
		if bestPrice == 0 || bestPrice > sumPrice {
			cheapestNpSet = nodePools
		}
	}
	return cheapestNpSet
}

func (e *Engine) findValuesBetween(attrValues []float64, min float64, max float64) ([]float64, error) {
	log.Debugf("finding values between: [%v, %v]", min, max)
	sort.Float64s(attrValues)
	if min > max {
		return nil, errors.New("min value cannot be larger than the max value")
	}

	if max < attrValues[0] {
		log.Debug("returning smallest value: %v", attrValues[0])
		return []float64{attrValues[0]}, nil
	} else if min > attrValues[len(attrValues)-1] {
		log.Debugf("returning largest value: %v", attrValues[len(attrValues)-1])
		return []float64{attrValues[len(attrValues)-1]}, nil
	}

	var values []float64

	for i := 0; i < len(attrValues); i++ {
		if attrValues[i] >= min && attrValues[i] <= max {
			values = append(values, attrValues[i])
		} else if attrValues[i] > max && len(values) < 1 {
			log.Debugf("couldn't find values between min and max, returning nearest values: [%v, %v]", attrValues[i-1], attrValues[i])
			return []float64{attrValues[i-1], attrValues[i]}, nil
		}
	}
	log.Debugf("returning values: %v", values)
	return values, nil
}

func avgNodeCount(values []float64, sum float64) int {
	var total float64
	for _, v := range values {
		total += v
	}
	avgValue := total / float64(len(values))
	return int(math.Ceil(sum / avgValue))
}

func findN(values []float64, sum float64) int {
	avg := avgNodeCount(values, sum)
	var N int
	switch {
	case avg <= 4:
		N = avg
	case avg <= 8:
		N = 4
	case avg <= 15:
		N = 5
	case avg <= 24:
		N = 6
	case avg <= 35:
		N = 7
	case avg > 35:
		N = 8
	}
	return N
}
