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
	"fmt"
	"math"
	"sort"

	"github.com/banzaicloud/cloudinfo/pkg/cloudinfo-client/models"
	"github.com/goph/emperror"
	"github.com/goph/logur"
	"github.com/pkg/errors"
)

const (
	// vm types - regular and ondemand means the same, they are both accepted on the API
	regular  = "regular"
	ondemand = "ondemand"
	spot     = "spot"
	// Memory represents the memory attribute for the recommender
	Memory = "memory"
	// Cpu represents the cpu attribute for the recommender
	Cpu = "cpu"

	recommenderErrorTag = "recommender"
)

// Engine represents the recommendation engine, it operates on a map of provider -> VmRegistry
type Engine struct {
	ciSource CloudInfoSource
	log      logur.Logger
}

// NewEngine creates a new Engine instance
func NewEngine(cis CloudInfoSource, log logur.Logger) *Engine {
	return &Engine{
		ciSource: cis,
		log:      log,
	}
}

// ClusterRecommendationReq encapsulates the recommendation input data
// swagger:parameters recommendClusterSetup
type ClusterRecommendationReq struct {
	// Total number of CPUs requested for the cluster
	SumCpu float64 `json:"sumCpu" binding:"min=1"`
	// Total memory requested for the cluster (GB)
	SumMem float64 `json:"sumMem" binding:"min=1"`
	// Minimum number of nodes in the recommended cluster
	MinNodes int `json:"minNodes,omitempty" binding:"min=1,ltefield=MaxNodes"`
	// Maximum number of nodes in the recommended cluster
	MaxNodes int `json:"maxNodes,omitempty"`
	// If true, recommended instance types will have a similar size
	SameSize bool `json:"sameSize,omitempty"`
	// Percentage of regular (on-demand) nodes in the recommended cluster
	OnDemandPct int `json:"onDemandPct,omitempty" binding:"min=0,max=100"`
	// Availability zones that the cluster should expand to
	Zones []string `json:"zones,omitempty"`
	// Total number of GPUs requested for the cluster
	SumGpu int `json:"sumGpu,omitempty"`
	// Are burst instances allowed in recommendation
	AllowBurst *bool `json:"allowBurst,omitempty"`
	// NetworkPerf specifies the network performance category
	NetworkPerf *string `json:"networkPerf" binding:"omitempty,networkPerf"`
	// Excludes is a blacklist - a slice with vm types to be excluded from the recommendation
	Excludes []string `json:"excludes,omitempty"`
	// Includes is a whitelist - a slice with vm types to be contained in the recommendation
	Includes []string `json:"includes,omitempty"`
	// AllowOlderGen allow older generations of virtual machines (applies for EC2 only)
	AllowOlderGen *bool `json:"allowOlderGen,omitempty"`
}

// ClusterScaleoutRecommendationReq encapsulates the recommendation input data
// swagger:parameters recommendClusterScaleOut
type ClusterScaleoutRecommendationReq struct {
	// Total desired number of CPUs in the cluster after the scale out
	DesiredCpu float64 `json:"desiredCpu" binding:"min=1"`
	// Total desired memory (GB) in the cluster after the scale out
	DesiredMem float64 `json:"desiredMem" binding:"min=1"`
	// Total desired number of GPUs in the cluster after the scale out
	DesiredGpu int `json:"desiredGpu" binding:"min=0"`
	// Percentage of regular (on-demand) nodes among the scale out nodes
	OnDemandPct int `json:"onDemandPct,omitempty" binding:"min=0,max=100"`
	// Availability zones to be included in the recommendation
	Zones []string `json:"zones,omitempty"`
	// Excludes is a blacklist - a slice with vm types to be excluded from the recommendation
	Excludes []string `json:"excludes,omitempty"`
	// Description of the current cluster layout
	// in:body
	ActualLayout []NodePoolDesc `json:"actualLayout" binding:"required"`
}

type NodePoolDesc struct {
	// Instance type of VMs in the node pool
	InstanceType string `json:"instanceType" binding:"required"`
	// Signals that the node pool consists of regular or spot/preemptible instance types
	VmClass string `json:"vmClass" binding:"required"`
	// Number of VMs in the node pool
	SumNodes int `json:"sumNodes" binding:"required"`
	// TODO: AZ?
	// Zones []string `json:"zones,omitempty" binding:"dive,zone"`
}

func (n NodePoolDesc) getVmClass() string {
	switch n.VmClass {
	case regular, spot:
		return n.VmClass
	case ondemand:
		return regular
	default:
		return spot
	}
}

// ClusterRecommendationResp encapsulates recommendation result data
// swagger:model RecommendationResponse
type ClusterRecommendationResp struct {
	// The cloud provider
	Provider string `json:"provider"`
	// Provider's service
	Service  string `json:"service"`
	// Service's region
	Region   string `json:"region"`
	// Availability zones in the recommendation - a multi-zone recommendation means that all node pools should expand to all zones
	Zones []string `json:"zones,omitempty"`
	// Recommended node pools
	NodePools []NodePool `json:"nodePools"`
	// Accuracy of the recommendation
	Accuracy ClusterRecommendationAccuracy `json:"accuracy"`
}

// NodePool represents a set of instances with a specific vm type
type NodePool struct {
	// Recommended virtual machine type
	VmType VirtualMachine `json:"vm"`
	// Recommended number of nodes in the node pool
	SumNodes int `json:"sumNodes"`
	// Specifies if the recommended node pool consists of regular or spot/preemptible instance types
	VmClass string `json:"vmClass"`
}

// ClusterRecommendationAccuracy encapsulates recommendation accuracy
type ClusterRecommendationAccuracy struct {
	// The summarised amount of memory in the recommended cluster
	RecMem float64 `json:"memory"`
	// Number of recommended cpus
	RecCpu float64 `json:"cpu"`
	// Number of recommended nodes
	RecNodes int `json:"nodes"`
	// Availability zones in the recommendation
	RecZone []string `json:"zone,omitempty"`
	// Amount of regular instance type prices in the recommended cluster
	RecRegularPrice float64 `json:"regularPrice"`
	// Number of regular instance type in the recommended cluster
	RecRegularNodes int `json:"regularNodes"`
	// Amount of spot instance type prices in the recommended cluster
	RecSpotPrice float64 `json:"spotPrice"`
	// Number of spot instance type in the recommended cluster
	RecSpotNodes int `json:"spotNodes"`
	// Total price in the recommended cluster
	RecTotalPrice float64 `json:"totalPrice"`
}

// VirtualMachine describes an instance type
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
	// Burst signals a burst type instance
	Burst bool `json:"burst"`
	// NetworkPerf holds the network performance
	NetworkPerf string `json:"networkPerf"`
	// NetworkPerfCat holds the network performance category
	NetworkPerfCat string `json:"networkPerfCategory"`
	// CurrentGen the vm is of current generation
	CurrentGen bool `json:"currentGen"`
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

// ByAvgPricePerCpu type for custom sorting of a slice of vms
type ByAvgPricePerCpu []VirtualMachine

func (a ByAvgPricePerCpu) Len() int      { return len(a) }
func (a ByAvgPricePerCpu) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByAvgPricePerCpu) Less(i, j int) bool {
	pricePerCpu1 := a[i].AvgPrice / a[i].Cpus
	pricePerCpu2 := a[j].AvgPrice / a[j].Cpus
	return pricePerCpu1 < pricePerCpu2
}

// ByAvgPricePerMemory type for custom sorting of a slice of vms
type ByAvgPricePerMemory []VirtualMachine

func (a ByAvgPricePerMemory) Len() int      { return len(a) }
func (a ByAvgPricePerMemory) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByAvgPricePerMemory) Less(i, j int) bool {
	pricePerMem1 := a[i].AvgPrice / a[i].Mem
	pricePerMem2 := a[j].AvgPrice / a[j].Mem
	return pricePerMem1 < pricePerMem2
}

type ByNonZeroNodePools []NodePool

func (a ByNonZeroNodePools) Len() int      { return len(a) }
func (a ByNonZeroNodePools) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByNonZeroNodePools) Less(i, j int) bool {
	return a[i].SumNodes > a[j].SumNodes
}

// RecommendCluster performs recommendation based on the provided arguments
func (e *Engine) RecommendCluster(provider string, service string, region string, req ClusterRecommendationReq, layoutDesc []NodePoolDesc, log logur.Logger) (*ClusterRecommendationResp, error) {
	e.log = log

	e.log.Info(fmt.Sprintf("recommending cluster configuration. request: [%#v]", req))

	attributes := []string{Cpu, Memory}
	nodePools := make(map[string][]NodePool, 2)

	desiredCpu := req.SumCpu
	desiredMem := req.SumMem
	desiredOdPct := req.OnDemandPct

	for _, attr := range attributes {
		var (
			values []float64
			err    error
		)
		if layoutDesc == nil {
			values, err = e.recommendAttrValues(provider, service, region, attr, req)
			if err != nil {
				return nil, emperror.Wrap(err, "failed to recommend attribute values")
			}
			e.log.Debug(fmt.Sprintf("recommended values for [%s]: count:[%d] , values: [%#v./te]", attr, len(values), values))
		}

		vmsInRange, err := e.findVmsWithAttrValues(provider, service, region, req.Zones, attr, values)
		if err != nil {
			return nil, emperror.With(err, recommenderErrorTag, "vms")
		}

		layout := e.transformLayout(layoutDesc, vmsInRange)
		if layout != nil {
			req.SumCpu, req.SumMem, req.OnDemandPct, err = e.computeScaleoutResources(layout, attr, desiredCpu, desiredMem, desiredOdPct)
			if err != nil {
				e.log.Error(emperror.Wrap(err, "failed to compute scaleout resources").Error())
				continue
			}
			if req.SumCpu < 0 && req.SumMem < 0 {
				return nil, emperror.With(fmt.Errorf("there's already enough resources in the cluster. Total resources available: CPU: %v, Mem: %v", desiredCpu-req.SumCpu, desiredMem-req.SumMem))
			}
		}

		vmFilters, _ := e.filtersForAttr(attr, provider)
		if err != nil {
			return nil, emperror.Wrap(err, "failed to identify filters")
		}
		odVms, spotVms, err := e.RecommendVms(vmsInRange, attr, vmFilters, req, layout)
		if err != nil {
			return nil, emperror.Wrap(err, "failed to recommend virtual machines")
		}

		if (len(odVms) == 0 && req.OnDemandPct > 0) || (len(spotVms) == 0 && req.OnDemandPct < 100) {
			e.log.Debug("no vms with the requested resources found", map[string]interface{}{"attribute": attr})
			// skip the nodepool creation, go to the next attr
			continue
		}
		e.log.Debug("recommended on-demand vms", map[string]interface{}{"attribute": attr, "count": len(odVms), "values": odVms})
		e.log.Debug("recommended spot vms", map[string]interface{}{"attribute": attr, "count": len(odVms), "values": odVms})

		//todo add request validation for interdependent request fields, eg: onDemandPct is always 100 when spot
		// instances are not available for provider
		if provider == "oracle" {
			e.log.Warn("onDemand percentage in the request ignored")
			req.OnDemandPct = 100
		}
		nps, err := e.RecommendNodePools(attr, odVms, spotVms, req, layout)
		if err != nil {
			return nil, emperror.Wrap(err, "failed to recommend nodepools")
		}
		e.log.Debug(fmt.Sprintf("recommended node pools for [%s]: count:[%d] , values: [%#v]", attr, len(nps), nps))

		nodePools[attr] = nps
	}

	if len(nodePools) == 0 {
		e.log.Debug(fmt.Sprintf("could not recommend node pools for request: %v", req))
		return nil, emperror.With(errors.New("could not recommend cluster with the requested resources"), recommenderErrorTag)
	}

	cheapestNodePoolSet := e.findCheapestNodePoolSet(nodePools)

	accuracy := findResponseSum(req.Zones, cheapestNodePoolSet)

	return &ClusterRecommendationResp{
		Provider:  provider,
		Service:   service,
		Region:    region,
		Zones:     req.Zones,
		NodePools: cheapestNodePoolSet,
		Accuracy:  accuracy,
	}, nil
}

func boolPointer(b bool) *bool {
	return &b
}

func (e *Engine) computeScaleoutResources(layout []NodePool, attr string, desiredCpu, desiredMem float64, desiredOdPct int) (float64, float64, int, error) {
	var currentCpuTotal, currentMemTotal, sumCurrentOdCpu, sumCurrentOdMem float64
	var scaleoutOdPct int
	for _, np := range layout {
		if np.VmClass == regular {
			sumCurrentOdCpu += float64(np.SumNodes) * np.VmType.Cpus
			sumCurrentOdMem += float64(np.SumNodes) * np.VmType.Mem
		}
		currentCpuTotal += float64(np.SumNodes) * np.VmType.Cpus
		currentMemTotal += float64(np.SumNodes) * np.VmType.Mem
	}

	scaleoutCpu := desiredCpu - currentCpuTotal
	scaleoutMem := desiredMem - currentMemTotal

	if scaleoutCpu < 0 && scaleoutMem < 0 {
		return scaleoutCpu, scaleoutMem, 0, nil
	}

	e.log.Debug(fmt.Sprintf("desiredCpu: %v, desiredMem: %v, currentCpuTotal/currentCpuOnDemand: %v/%v, currentMemTotal/currentMemOnDemand: %v/%v", desiredCpu, desiredMem, currentCpuTotal, sumCurrentOdCpu, currentMemTotal, sumCurrentOdMem))
	e.log.Debug(fmt.Sprintf("total scaleout cpu/mem needed: %v/%v", scaleoutCpu, scaleoutMem))
	e.log.Debug(fmt.Sprintf("desired on-demand percentage: %v", desiredOdPct))

	switch attr {
	case Cpu:
		if scaleoutCpu < 0 {
			return 0, 0, 0, errors.New("there's already enough CPU resources in the cluster")
		}
		desiredOdCpu := desiredCpu * float64(desiredOdPct) / 100
		scaleoutOdCpu := desiredOdCpu - sumCurrentOdCpu
		scaleoutOdPct = int(scaleoutOdCpu / scaleoutCpu * 100)
		e.log.Debug(fmt.Sprintf("desired on-demand cpu: %v, cpu to add with the scaleout: %v", desiredOdCpu, scaleoutOdCpu))
	case Memory:
		if scaleoutMem < 0 {
			return 0, 0, 0, emperror.With(errors.New("there's already enough memory resources in the cluster"))
		}
		desiredOdMem := desiredMem * float64(desiredOdPct) / 100
		scaleoutOdMem := desiredOdMem - sumCurrentOdMem
		e.log.Debug(fmt.Sprintf("desired on-demand memory: %v, memory to add with the scaleout: %v", desiredOdMem, scaleoutOdMem))
		scaleoutOdPct = int(scaleoutOdMem / scaleoutMem * 100)
	}
	if scaleoutOdPct > 100 {
		// even if we add only on-demand instances, we still we can't reach the minimum ratio
		return 0, 0, 0, emperror.With(errors.New("couldn't scale out cluster with the provided parameters"), "onDemandPct", desiredOdPct)
	} else if scaleoutOdPct < 0 {
		// means that we already have enough resources in the cluster to keep the minimum ratio
		scaleoutOdPct = 0
	}
	e.log.Debug(fmt.Sprintf("percentage of on-demand resources in the scaleout: %v", scaleoutOdPct))
	return scaleoutCpu, scaleoutMem, scaleoutOdPct, nil
}

// RecommendClusterScaleOut performs recommendation for an existing layout's scale out
func (e *Engine) RecommendClusterScaleOut(provider string, service string, region string, req ClusterScaleoutRecommendationReq, log logur.Logger) (*ClusterRecommendationResp, error) {
	log.Info(fmt.Sprintf("recommending cluster configuration. request: [%#v]", req))

	includes := make([]string, len(req.ActualLayout))
	for i, npd := range req.ActualLayout {
		includes[i] = npd.InstanceType
	}

	clReq := ClusterRecommendationReq{
		Zones:         req.Zones,
		AllowBurst:    boolPointer(true),
		Includes:      includes,
		Excludes:      req.Excludes,
		AllowOlderGen: boolPointer(true),
		MaxNodes:      math.MaxInt8,
		MinNodes:      1,
		NetworkPerf:   nil,
		OnDemandPct:   req.OnDemandPct,
		SameSize:      false,
		SumCpu:        req.DesiredCpu,
		SumMem:        req.DesiredMem,
		SumGpu:        req.DesiredGpu,
	}

	return e.RecommendCluster(provider, service, region, clReq, req.ActualLayout, log)
}

func findResponseSum(zones []string, nodePoolSet []NodePool) ClusterRecommendationAccuracy {
	var sumCpus float64
	var sumMem float64
	var sumNodes int
	var sumRegularPrice float64
	var sumRegularNodes int
	var sumSpotPrice float64
	var sumSpotNodes int
	var sumTotalPrice float64
	for _, nodePool := range nodePoolSet {
		sumCpus += nodePool.getSum(Cpu)
		sumMem += nodePool.getSum(Memory)
		sumNodes += nodePool.SumNodes
		if nodePool.VmClass == regular {
			sumRegularPrice += nodePool.poolPrice()
			sumRegularNodes += nodePool.SumNodes
		} else {
			sumSpotPrice += nodePool.poolPrice()
			sumSpotNodes += nodePool.SumNodes
		}
		sumTotalPrice += nodePool.poolPrice()
	}

	return ClusterRecommendationAccuracy{
		RecCpu:          sumCpus,
		RecMem:          sumMem,
		RecNodes:        sumNodes,
		RecZone:         zones,
		RecRegularPrice: sumRegularPrice,
		RecRegularNodes: sumRegularNodes,
		RecSpotPrice:    sumSpotPrice,
		RecSpotNodes:    sumSpotNodes,
		RecTotalPrice:   sumTotalPrice,
	}
}

// findCheapestNodePoolSet looks up the "cheapest" node pool set from the provided map
func (e *Engine) findCheapestNodePoolSet(nodePoolSets map[string][]NodePool) []NodePool {
	e.log.Info("finding cheapest pool set...")
	var cheapestNpSet []NodePool
	var bestPrice float64

	for attr, nodePools := range nodePoolSets {
		var sumPrice float64
		var sumCpus float64
		var sumMem float64

		for _, np := range nodePools {
			sumPrice += np.poolPrice()
			sumCpus += np.getSum(Cpu)
			sumMem += np.getSum(Memory)
		}
		e.log.Debug("checking node pool",
			map[string]interface{}{"attribute": attr, "cpu": sumCpus, "memory": sumMem, "price": sumPrice})

		if bestPrice == 0 || bestPrice > sumPrice {
			e.log.Debug("cheaper node pool set is found", map[string]interface{}{"price": sumPrice})
			bestPrice = sumPrice
			cheapestNpSet = nodePools
		}
	}
	return cheapestNpSet
}

func avgSpotNodeCount(minNodes, maxNodes, odNodes int) int {
	count := float64(minNodes-odNodes+maxNodes-odNodes) / 2
	spotCount := int(math.Ceil(count))
	if spotCount < 0 {
		return 0
	}
	return spotCount
}

// findN returns the number of nodes required
func findN(avg int) int {
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

func findM(N int, spotVms []VirtualMachine) int {
	if N > 0 {
		return int(math.Min(math.Ceil(float64(N)*1.5), float64(len(spotVms))))
	} else {
		return int(math.Min(3, float64(len(spotVms))))
	}
}

// RecommendVms selects a slice of VirtualMachines for the given attribute and requirements in the request
func (e *Engine) RecommendVms(vms []VirtualMachine, attr string, filters []vmFilter, req ClusterRecommendationReq, layout []NodePool) ([]VirtualMachine, []VirtualMachine, error) {
	e.log.Info("recommending virtual machines", map[string]interface{}{"attribute": attr})

	var filteredVms []VirtualMachine
	for _, vm := range vms {
		if e.filtersApply(vm, filters, req) {
			filteredVms = append(filteredVms, vm)
		}
	}

	if len(filteredVms) == 0 {
		e.log.Debug("no virtual machines found", map[string]interface{}{"attribute": attr})
		return []VirtualMachine{}, []VirtualMachine{}, nil
	}

	var odVms, spotVms []VirtualMachine
	if layout == nil {
		odVms, spotVms = filteredVms, filteredVms
	} else {
		for _, np := range layout {
			for _, vm := range filteredVms {
				if np.VmType.Type == vm.Type {
					if np.VmClass == regular {
						odVms = append(odVms, vm)
					} else {
						spotVms = append(spotVms, vm)
					}
					continue
				}
			}
		}
	}

	if req.OnDemandPct < 100 {
		// retain only the nodes that are available as spot instances
		spotVms = e.filterSpots(spotVms)
		if len(spotVms) == 0 {
			e.log.Debug("no vms suitable for spot pools", map[string]interface{}{"attribute": attr})
			return []VirtualMachine{}, []VirtualMachine{}, nil
		}
	}
	return odVms, spotVms, nil

}

func (e *Engine) findVmsWithAttrValues(provider string, service string, region string, zones []string, attr string, values []float64) ([]VirtualMachine, error) {
	var err error
	e.log.Info("looking for instance types", map[string]interface{}{"attribute": attr, "values": values})
	var (
		vms []VirtualMachine
	)

	if len(zones) == 0 {
		if zones, err = e.ciSource.GetZones(provider, service, region); err != nil {
			return nil, err
		}
	}

	allProducts, err := e.ciSource.GetProductDetails(provider, service, region)
	if err != nil {
		return nil, err
	}

	for _, p := range allProducts {
		included := true
		if len(values) > 0 {
			included = false
			for _, v := range values {
				switch attr {
				case Cpu:
					if p.Cpus == v {
						included = true
						continue
					}
				case Memory:
					if p.Mem == v {
						included = true
						continue
					}
				default:
					return nil, errors.New("unsupported attribute")
				}
			}
		}
		if included {
			vms = append(vms, VirtualMachine{
				Type:           p.Type,
				OnDemandPrice:  p.OnDemandPrice,
				AvgPrice:       avg(p.SpotPrice, zones),
				Cpus:           p.Cpus,
				Mem:            p.Mem,
				Gpus:           p.Gpus,
				Burst:          p.Burst,
				NetworkPerf:    p.NtwPerf,
				NetworkPerfCat: p.NtwPerfCat,
				CurrentGen:     p.CurrentGen,
			})
		}
	}

	e.log.Debug("found vms", map[string]interface{}{attr: values, "vms": vms})
	return vms, nil
}

func (e *Engine) transformLayout(layoutDesc []NodePoolDesc, vms []VirtualMachine) []NodePool {
	if layoutDesc == nil {
		return nil
	}
	nps := make([]NodePool, len(layoutDesc))
	for i, npd := range layoutDesc {
		for _, vm := range vms {
			if vm.Type == npd.InstanceType {
				nps[i] = NodePool{
					VmType:   vm,
					VmClass:  npd.getVmClass(),
					SumNodes: npd.SumNodes,
				}
				break
			}
		}
	}
	return nps
}

func avg(prices []*models.ZonePrice, recZones []string) float64 {
	if len(prices) == 0 {
		return 0.0
	}
	avgPrice := 0.0
	for _, price := range prices {
		for _, z := range recZones {
			if z == price.Zone {
				avgPrice += price.Price
			}
		}
	}
	return avgPrice / float64(len(prices))
}

// filtersApply returns true if all the filters apply for the given vm
func (e *Engine) filtersApply(vm VirtualMachine, filters []vmFilter, req ClusterRecommendationReq) bool {

	for _, filter := range filters {
		if !filter(vm, req) {
			// one of the filters doesn't apply - quit the iteration
			return false
		}
	}
	// no filters or applies
	return true
}

// recommendAttrValues selects the attribute values allowed to participate in the recommendation process
func (e *Engine) recommendAttrValues(provider string, service string, region string, attr string, req ClusterRecommendationReq) ([]float64, error) {

	allValues, err := e.ciSource.GetAttributeValues(provider, service, region, attr)
	if err != nil {
		return nil, err
	}

	e.log.Debug("selecting attributes", map[string]interface{}{"attribute": attr, "values": allValues})
	values, err := AttributeValues(allValues).SelectAttributeValues(req.minValuePerVm(attr), req.maxValuePerVm(attr))
	if err != nil {
		return nil, emperror.With(err, recommenderErrorTag, "attributes")
	}

	return values, nil
}

// filtersForAttr returns the slice for
func (e *Engine) filtersForAttr(attr string, provider string) ([]vmFilter, error) {
	// generic filters - not depending on providers and attributes
	var filters = []vmFilter{e.includesFilter, e.excludesFilter}

	// provider specific filters
	switch provider {
	case "amazon":
		filters = append(filters, e.currentGenFilter, e.burstFilter, e.ntwPerformanceFilter)
	case "google":
		filters = append(filters, e.ntwPerformanceFilter)
	}

	// attribute specific filters
	switch attr {
	case Cpu:
		filters = append(filters, e.minMemRatioFilter)
	case Memory:
		filters = append(filters, e.minCpuRatioFilter)
	default:
		return nil, emperror.With(errors.New("unsupported attribute"), "attribute", attr)
	}

	return filters, nil
}

// sortByAttrValue returns the slice for
func (e *Engine) sortByAttrValue(attr string, vms []VirtualMachine) {
	// sort and cut
	switch attr {
	case Memory:
		sort.Sort(ByAvgPricePerMemory(vms))
	case Cpu:
		sort.Sort(ByAvgPricePerCpu(vms))
	default:
		e.log.Error("unsupported attribute", map[string]interface{}{"attribute": attr})
	}
}

// RecommendNodePools finds the slice of NodePools that may participate in the recommendation process
func (e *Engine) RecommendNodePools(attr string, odVms []VirtualMachine, spotVms []VirtualMachine, req ClusterRecommendationReq, layout []NodePool) ([]NodePool, error) {

	e.log.Debug(fmt.Sprintf("requested sum for attribute [%s]: [%f]", attr, req.sum(attr)))
	var sumOnDemandValue = req.sum(attr) * float64(req.OnDemandPct) / 100
	e.log.Debug(fmt.Sprintf("on demand sum value for attr [%s]: [%f]", attr, sumOnDemandValue))

	// recommend on-demands
	odNps := make([]NodePool, 0)

	//TODO: validate if there's no on-demand in layout but we want to add ondemands
	for _, np := range layout {
		if np.VmClass == regular {
			odNps = append(odNps, np)
		}
	}
	var actualOnDemandResources float64
	var odNodesToAdd int
	if len(odVms) > 0 {
		// find cheapest onDemand instance from the list - based on price per attribute
		selectedOnDemand := odVms[0]
		for _, vm := range odVms {
			if vm.OnDemandPrice/vm.getAttrValue(attr) < selectedOnDemand.OnDemandPrice/selectedOnDemand.getAttrValue(attr) {
				selectedOnDemand = vm
			}
		}
		odNodesToAdd = int(math.Ceil(sumOnDemandValue / selectedOnDemand.getAttrValue(attr)))
		if layout == nil {
			odNps = append(odNps, NodePool{
				SumNodes: odNodesToAdd,
				VmClass:  regular,
				VmType:   selectedOnDemand,
			})
		} else {
			for i, np := range odNps {
				if np.VmType.Type == selectedOnDemand.Type {
					odNps[i].SumNodes += odNodesToAdd
				}
			}
		}
		actualOnDemandResources = selectedOnDemand.getAttrValue(attr) * float64(odNodesToAdd)
	}

	// recalculate required spot resources by taking actual on-demand resources into account
	var sumSpotValue = req.sum(attr) - actualOnDemandResources
	e.log.Debug(fmt.Sprintf("spot sum value for attr [%s]: [%f]", attr, sumSpotValue))

	// recommend spot pools
	spotNps := make([]NodePool, 0)
	excludedSpotNps := make([]NodePool, 0)

	e.sortByAttrValue(attr, spotVms)

	var N int
	if layout == nil {
		// the "magic" number of machines for diversifying the types
		N = int(math.Min(float64(findN(avgSpotNodeCount(req.MinNodes, req.MaxNodes, odNodesToAdd))), float64(len(spotVms))))
		// the second "magic" number for diversifying the layout
		M := findM(N, spotVms)
		e.log.Debug(fmt.Sprintf("Magic 'Marton' numbers: N=%d, M=%d", N, M))

		// the first M vm-s
		recommendedVms := spotVms[:M]

		// create spot nodepools - one for the first M vm-s
		for _, vm := range recommendedVms {
			spotNps = append(spotNps, NodePool{
				SumNodes: 0,
				VmClass:  spot,
				VmType:   vm,
			})
		}
	} else {
		sort.Sort(ByNonZeroNodePools(layout))
		var nonZeroNPs int
		for _, np := range layout {
			if np.VmClass == spot {
				if np.SumNodes > 0 {
					nonZeroNPs += 1
				}
				included := false
				for _, vm := range spotVms {
					if np.VmType.Type == vm.Type {
						spotNps = append(spotNps, np)
						included = true
						break
					}
				}
				if !included {
					excludedSpotNps = append(excludedSpotNps, np)
				}
			}
		}
		N = findNWithLayout(nonZeroNPs, len(spotVms))
		e.log.Debug(fmt.Sprintf("Magic 'Marton' number: N=%d", N))
	}
	e.log.Debug(fmt.Sprintf("created [%d] regular and [%d] spot price node pools", len(odNps), len(spotNps)))
	spotNps = e.fillSpotNodePools(sumSpotValue, N, spotNps, attr)
	if len(excludedSpotNps) > 0 {
		spotNps = append(spotNps, excludedSpotNps...)
	}
	return append(odNps, spotNps...), nil
}

func findNWithLayout(nonZeroNps, vmOptions int) int {
	// vmOptions cannot be 0 because validation would fail sooner
	if nonZeroNps == 0 {
		return 1
	}
	if nonZeroNps < vmOptions {
		return nonZeroNps
	} else {
		return vmOptions
	}
}

func (e *Engine) fillSpotNodePools(sumSpotValue float64, N int, nps []NodePool, attr string) []NodePool {
	var (
		sumValueInPools, minValue float64
		idx, minIndex             int
	)
	for i := 0; i < N; i++ {
		v := float64(nps[i].SumNodes) * nps[i].VmType.getAttrValue(attr)
		sumValueInPools += v
		if i == 0 {
			minValue = v
			minIndex = i
		} else if v < minValue {
			minValue = v
			minIndex = i
		}
	}
	desiredSpotValue := sumValueInPools + sumSpotValue
	idx = minIndex
	for sumValueInPools < desiredSpotValue {
		nodePoolIdx := idx % N
		if nodePoolIdx == minIndex {
			// always add a new instance to the option with the lowest attribute value to balance attributes and move on
			nps[nodePoolIdx].SumNodes += 1
			sumValueInPools += nps[nodePoolIdx].VmType.getAttrValue(attr)
			e.log.Debug(fmt.Sprintf("adding vm to the [%d]th (min sized) node pool, sum value in pools: [%f]", nodePoolIdx, sumValueInPools))
			idx++
		} else if nps[nodePoolIdx].getNextSum(attr) > nps[minIndex].getSum(attr) {
			// for other pools, if adding another vm would exceed the current sum of the cheapest option, move on to the next one
			e.log.Debug(fmt.Sprintf("skip adding vm to the [%d]th node pool", nodePoolIdx))
			idx++
		} else {
			// otherwise add a new one, but do not move on to the next one
			nps[nodePoolIdx].SumNodes += 1
			sumValueInPools += nps[nodePoolIdx].VmType.getAttrValue(attr)
			e.log.Debug(fmt.Sprintf("adding vm to the [%d]th node pool, sum value in pools: [%f]", nodePoolIdx, sumValueInPools))
		}
	}
	return nps
}

// maxValuePerVm calculates the maximum value per node for the given attribute
func (req *ClusterRecommendationReq) maxValuePerVm(attr string) float64 {
	switch attr {
	case Cpu:
		return req.SumCpu / float64(req.MinNodes)
	case Memory:
		return req.SumMem / float64(req.MinNodes)
	default:
		return 0
	}
}

// minValuePerVm calculates the minimum value per node for the given attribute
func (req *ClusterRecommendationReq) minValuePerVm(attr string) float64 {
	switch attr {
	case Cpu:
		return req.SumCpu / float64(req.MaxNodes)
	case Memory:
		return req.SumMem / float64(req.MaxNodes)
	default:
		return 0
	}
}

// gets the requested sum for the attribute value
func (req *ClusterRecommendationReq) sum(attr string) float64 {
	switch attr {
	case Cpu:
		return req.SumCpu
	case Memory:
		return req.SumMem
	default:
		return 0
	}
}

// getSum gets the total value for the given attribute per pool
func (n *NodePool) getSum(attr string) float64 {
	return float64(n.SumNodes) * n.VmType.getAttrValue(attr)
}

// getNextSum gets the total value if the pool was increased by one
func (n *NodePool) getNextSum(attr string) float64 {
	return n.getSum(attr) + n.VmType.getAttrValue(attr)
}

// poolPrice calculates the price of the pool
func (n *NodePool) poolPrice() float64 {
	var sum = float64(0)
	switch n.VmClass {
	case regular:
		sum = float64(n.SumNodes) * n.VmType.OnDemandPrice
	case spot:
		sum = float64(n.SumNodes) * n.VmType.AvgPrice
	}
	return sum
}

func contains(slice []string, s string) bool {
	for _, e := range slice {
		if e == s {
			return true
		}
	}
	return false
}
