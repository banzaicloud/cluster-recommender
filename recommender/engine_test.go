package recommender

import (
	"testing"

	"errors"
	"github.com/stretchr/testify/assert"
)

type dummyVmRegistry struct {
	// implement the interface
	VmRegistry
}

var vms = []VirtualMachine{
	{OnDemandPrice: float64(10), AvgPrice: 99, Cpus: float64(10), Mem: float64(10), Gpus: float64(0)},
	{OnDemandPrice: float64(12), AvgPrice: 89, Cpus: float64(10), Mem: float64(10), Gpus: float64(0)},
	{OnDemandPrice: float64(21), AvgPrice: 92, Cpus: float64(12), Mem: float64(12), Gpus: float64(0)},
}
var vme = []VirtualMachine{
	{OnDemandPrice: float64(10), AvgPrice: 99, Cpus: float64(4), Mem: float64(10), Gpus: float64(0)},
	{OnDemandPrice: float64(12), AvgPrice: 89, Cpus: float64(5), Mem: float64(10), Gpus: float64(0)},
	{OnDemandPrice: float64(21), AvgPrice: 92, Cpus: float64(3), Mem: float64(12), Gpus: float64(0)},
}

func TestNewEngine(t *testing.T) {

	tests := []struct {
		name         string
		vmRegistries map[string]VmRegistry
		checker      func(engine *Engine, err error)
	}{
		{
			name:         "engine successfully created",
			vmRegistries: map[string]VmRegistry{"ec2": dummyVmRegistry{}},
			checker: func(engine *Engine, err error) {
				assert.Nil(t, err, "should not get error ")
				assert.NotNil(t, engine, "the engine should not be nil")
			},
		},
		{
			name:         "engine creation fails when registries is nil",
			vmRegistries: nil,
			checker: func(engine *Engine, err error) {
				assert.Nil(t, engine, "the engine should be nil")
				assert.NotNil(t, err, "the error shouldn't be nil")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.checker(NewEngine(test.vmRegistries))

		})
	}
}

// utility VmRegistry for mocking purposes
type DummyVmRegistry struct {
	// test case id to drive the behaviour
	TcId int
}

func (dvmr DummyVmRegistry) getAvailableAttributeValues(attr string) ([]float64, error) {
	switch dvmr.TcId {
	case 1:
		// 3 values between 10 - 20
		return []float64{12, 13, 14}, nil
	case 2:
		// 2 values between 10 - 20
		return []float64{8, 13, 14, 6}, nil
	case 3:
		// no values between 10-20, return the closest value
		return []float64{30, 40, 50, 60}, nil
	case 4:
		// no values between 10-20, return the closest value
		return []float64{1, 2, 3, 5, 9}, nil
	case 5:
		// error, min > max
		return []float64{1}, nil
	case 6:
		// error returned
		return nil, errors.New("")

	}

	return nil, nil
}

func (dvmr DummyVmRegistry) findVmsWithAttrValues(region string, zones []string, attr string, values []float64) ([]VirtualMachine, error) {
	switch dvmr.TcId {
	case 2:
		return vme, nil
	case 7:
		return nil, errors.New("attribute value error")
	}
	return vms, nil
}

func TestEngine_RecommendAttrValues(t *testing.T) {

	tests := []struct {
		name         string
		vmRegistries map[string]VmRegistry
		request      ClusterRecommendationReq
		provider     string
		attribute    string
		check        func([]float64, error)
	}{
		{
			name:         "all attributes between limits",
			vmRegistries: map[string]VmRegistry{"ec2": DummyVmRegistry{TcId: 1}},
			request: ClusterRecommendationReq{
				MinNodes: 5,
				MaxNodes: 10,
				SumMem:   100,
				SumCpu:   100,
			},
			provider:  "ec2",
			attribute: Cpu,

			check: func(values []float64, err error) {
				assert.Nil(t, err, "should not get error when recommending attributes")
				assert.Equal(t, 3, len(values), "recommended number of values is not as expected")

			},
		},
		{
			name:         "attributes out of limits not recommended",
			vmRegistries: map[string]VmRegistry{"ec2": DummyVmRegistry{TcId: 2}},
			request: ClusterRecommendationReq{
				MinNodes: 5,
				MaxNodes: 10,
				SumMem:   100,
				SumCpu:   100,
			},
			provider:  "ec2",
			attribute: Cpu,

			check: func(values []float64, err error) {
				assert.Nil(t, err, "should not get error when recommending attributes")
				assert.Equal(t, 2, len(values), "recommended number of values is not as expected")

			},
		},
		{
			name:         "no values between limits found - smallest value returned",
			vmRegistries: map[string]VmRegistry{"ec2": DummyVmRegistry{TcId: 3}},
			request: ClusterRecommendationReq{
				MinNodes: 5,
				MaxNodes: 10,
				SumMem:   100,
				SumCpu:   100,
			},
			provider:  "ec2",
			attribute: Cpu,

			check: func(values []float64, err error) {
				assert.Nil(t, err, "should not get error when recommending attributes")
				assert.Equal(t, 1, len(values), "recommended number of values is not as expected")
				assert.Equal(t, float64(30), values[0], "recommended number of values is not as expected")

			},
		},
		{
			name:         "no values between limits found - largest value returned",
			vmRegistries: map[string]VmRegistry{"ec2": DummyVmRegistry{TcId: 4}},
			request: ClusterRecommendationReq{
				MinNodes: 5,
				MaxNodes: 10,
				SumMem:   100,
				SumCpu:   100,
			},
			provider:  "ec2",
			attribute: Cpu,

			check: func(values []float64, err error) {
				assert.Nil(t, err, "should not get error when recommending attributes")
				assert.Equal(t, 1, len(values), "recommended number of values is not as expected")
				assert.Equal(t, float64(9), values[0], "recommended number of values is not as expected")

			},
		},
		{
			name:         "error - min larger than max",
			vmRegistries: map[string]VmRegistry{"ec2": DummyVmRegistry{TcId: 5}},
			request: ClusterRecommendationReq{
				MinNodes: 10,
				MaxNodes: 5,
				SumMem:   100,
				SumCpu:   100,
			},
			provider:  "ec2",
			attribute: Cpu,

			check: func(values []float64, err error) {
				assert.Equal(t, err.Error(), "min value cannot be larger than the max value")

			},
		},
		{
			name:         "error - no values provided",
			vmRegistries: map[string]VmRegistry{"ec2": DummyVmRegistry{TcId: 100}},
			request: ClusterRecommendationReq{
				MinNodes: 10,
				MaxNodes: 10,
				SumMem:   100,
				SumCpu:   100,
			},
			provider:  "ec2",
			attribute: Cpu,

			check: func(values []float64, err error) {
				assert.Equal(t, err.Error(), "no attribute values provided")

			},
		},
		{
			name:         "error - attribute values could not be retrieved",
			vmRegistries: map[string]VmRegistry{"ec2": DummyVmRegistry{TcId: 6}},
			request: ClusterRecommendationReq{
				MinNodes: 10,
				MaxNodes: 10,
				SumMem:   100,
				SumCpu:   100,
			},
			provider:  "ec2",
			attribute: Cpu,

			check: func(values []float64, err error) {
				assert.Nil(t, values, "returned attr values should be nils")
				assert.NotNil(t, err.Error(), "no attribute values provided")

			},
		},
		{
			name:         "error - unsupported attribute",
			vmRegistries: map[string]VmRegistry{"ec2": DummyVmRegistry{TcId: 1}},
			request: ClusterRecommendationReq{
				MinNodes: 5,
				MaxNodes: 10,
				SumMem:   100,
				SumCpu:   100,
			},
			provider:  "ec2",
			attribute: "error",

			check: func(values []float64, err error) {
				assert.Nil(t, values, "the values should be nil")
				assert.EqualError(t, err, "unsupported attribute: [error]")

			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine, err := NewEngine(test.vmRegistries)
			assert.Nil(t, err, "the engine couldn't be created")

			test.check(engine.RecommendAttrValues(engine.VmRegistries["ec2"].(VmRegistry), test.attribute, test.request))

		})
	}
}

func TestEngine_RecommendVms(t *testing.T) {
	tests := []struct {
		name         string
		region       string
		vmRegistries map[string]VmRegistry
		values       []float64
		filters      []vmFilter
		request      ClusterRecommendationReq
		provider     string
		attribute    string
		check        func([]VirtualMachine, error)
	}{
		{
			name:         "error - findVmsWithAttrValues",
			region:       "us-west-2",
			vmRegistries: map[string]VmRegistry{"ec2": DummyVmRegistry{TcId: 7}},
			check: func(vms []VirtualMachine, err error) {
				assert.Equal(t, err, errors.New("attribute value error"))
				assert.Nil(t, vms, "the vms should be nil")

			},
		},
		{
			name:   "success - recommend vms",
			region: "us-west-2",
			filters: []vmFilter{func(vm VirtualMachine, req ClusterRecommendationReq) bool {
				return true
			}},
			vmRegistries: map[string]VmRegistry{"ec2": DummyVmRegistry{}},
			provider:     "ec2",
			attribute:    Cpu,

			check: func(vms []VirtualMachine, err error) {
				assert.Nil(t, err, "the error should be nil")
				assert.Equal(t, 3, len(vms))

			},
		},
		{
			name:   "could not find any VMs to recommender",
			region: "us-west-2",
			filters: []vmFilter{func(vm VirtualMachine, req ClusterRecommendationReq) bool {
				return false
			}},
			vmRegistries: map[string]VmRegistry{"ec2": DummyVmRegistry{}},
			provider:     "ec2",
			attribute:    Cpu,

			check: func(vms []VirtualMachine, err error) {
				assert.Equal(t, err, errors.New("couldn't find any VMs to recommend"))
				assert.Nil(t, vms, "the vms should be nil")

			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine, err := NewEngine(test.vmRegistries)
			assert.Nil(t, err, "the engine couldn't be created")

			test.check(engine.RecommendVms(engine.VmRegistries["ec2"].(VmRegistry), test.region, test.attribute, test.values, test.filters, test.request))

		})
	}
}

func TestEngine_RecommendNodePools(t *testing.T) {
	tests := []struct {
		name         string
		vmRegistries map[string]VmRegistry
		attr         string
		vms          []VirtualMachine
		values       []float64
		request      ClusterRecommendationReq
		check        func([]NodePool, error)
	}{
		{
			name:         "successful",
			vmRegistries: map[string]VmRegistry{"ec2": DummyVmRegistry{TcId: 1}},
			vms:          vms,
			attr:         Cpu,
			values:       []float64{4},
			request: ClusterRecommendationReq{
				MinNodes: 5,
				MaxNodes: 10,
				SumMem:   100,
				SumCpu:   100,
			},
			check: func(nps []NodePool, err error) {
				assert.Nil(t, err, "the error should be nil")
				assert.NotNil(t, nps, "the nps shouldn't be nil")

			},
		},
		{
			name:         "attribute error",
			vmRegistries: map[string]VmRegistry{"ec2": DummyVmRegistry{TcId: 1}},
			vms:          vms,
			attr:         "error",
			values:       []float64{4},
			request: ClusterRecommendationReq{
				MinNodes: 5,
				MaxNodes: 10,
				SumMem:   100,
				SumCpu:   100,
			},
			check: func(nps []NodePool, err error) {
				assert.Equal(t, err, errors.New("could not get sum for attr: [error], cause: [unsupported attribute: [error]]"))
				assert.Nil(t, nps, "the nps should be nil")

			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine, err := NewEngine(test.vmRegistries)
			assert.Nil(t, err, "the engine couldn't be created")

			test.check(engine.RecommendNodePools(test.attr, test.vms, test.values, test.request))

		})
	}
}

func TestEngine_RecommendCluster(t *testing.T) {
	tests := []struct {
		name         string
		vmRegistries map[string]VmRegistry
		request      ClusterRecommendationReq
		provider     string
		region       string
		check        func(response *ClusterRecommendationResp, err error)
	}{
		{
			name:         "cluster recommendation success",
			vmRegistries: map[string]VmRegistry{"ec2": DummyVmRegistry{TcId: 1}},
			request: ClusterRecommendationReq{
				MinNodes: 5,
				MaxNodes: 10,
				SumMem:   100,
				SumCpu:   100,
				Zones:    []string{"testZone1", "testZone2"},
			},
			provider: "ec2",
			region:   "us-west-2",
			check: func(response *ClusterRecommendationResp, err error) {
				assert.Nil(t, err, "should not get error when recommending")
				assert.NotNil(t, response, "the response shouldn't be nil")
			},
		},
		{
			name:         "error - RecommendAttrValues, min value cannot be larger than the max value",
			vmRegistries: map[string]VmRegistry{"ec2": DummyVmRegistry{TcId: 1}},
			request: ClusterRecommendationReq{
				MinNodes: 10,
				MaxNodes: 5,
				SumMem:   100,
				SumCpu:   100,
			},
			provider: "ec2",
			region:   "us-west-2",
			check: func(response *ClusterRecommendationResp, err error) {
				assert.Equal(t, err, errors.New("could not get values for attr: [cpu], cause: [min value cannot be larger than the max value]"))
				assert.Nil(t, response, "the response should be nil")
			},
		},
		{
			name:         "error - RecommendVms, could not find any VMs to recommend",
			vmRegistries: map[string]VmRegistry{"ec2": DummyVmRegistry{TcId: 2}},
			request: ClusterRecommendationReq{
				MinNodes: 5,
				MaxNodes: 10,
				SumMem:   100,
				SumCpu:   100,
			},
			provider: "ec2",
			region:   "us-west-2",
			check: func(response *ClusterRecommendationResp, err error) {
				assert.Equal(t, err, errors.New("could not get virtual machines for attr: [cpu], cause: [couldn't find any VMs to recommend]"))
				assert.Nil(t, response, "the response should be nil")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine, err := NewEngine(test.vmRegistries)
			assert.Nil(t, err, "the engine couldn't be created")

			test.check(engine.RecommendCluster(test.provider, test.region, test.request))

		})
	}
}

func TestEngine_sortByAttrValue(t *testing.T) {
	tests := []struct {
		name         string
		vmRegistries map[string]VmRegistry
		attr         string
		vms          []VirtualMachine
		check        func(err error)
	}{
		{
			name:         "error - unsupported attribute",
			vmRegistries: map[string]VmRegistry{"ec2": DummyVmRegistry{}},
			attr:         "error",
			vms:          vms,
			check: func(err error) {
				assert.EqualError(t, err, "unsupported attribute: [error]")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine, err := NewEngine(test.vmRegistries)
			assert.Nil(t, err, "the engine couldn't be created")

			test.check(engine.sortByAttrValue(test.attr, test.vms))

		})
	}
}

func TestEngine_filtersForAttr(t *testing.T) {
	tests := []struct {
		name         string
		vmRegistries map[string]VmRegistry
		attr         string
		check        func(vms []vmFilter, err error)
	}{
		{
			name:         "error - unsupported attribute",
			vmRegistries: map[string]VmRegistry{"ec2": DummyVmRegistry{}},
			attr:         "error",
			check: func(vms []vmFilter, err error) {
				assert.EqualError(t, err, "unsupported attribute: [error]")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine, err := NewEngine(test.vmRegistries)
			assert.Nil(t, err, "the engine couldn't be created")

			test.check(engine.filtersForAttr(test.attr))

		})
	}
}
