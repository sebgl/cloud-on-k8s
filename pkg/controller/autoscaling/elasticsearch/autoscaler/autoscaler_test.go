// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package autoscaler

import (
	"testing"

	esv1 "github.com/elastic/cloud-on-k8s/pkg/apis/elasticsearch/v1"
	"github.com/elastic/cloud-on-k8s/pkg/controller/autoscaling/elasticsearch/resources"
	"github.com/elastic/cloud-on-k8s/pkg/controller/autoscaling/elasticsearch/status"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/client"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
)

func Test_GetResources(t *testing.T) {
	defaultNodeSets := esv1.NodeSetList{{
		Name: "default",
	}}
	type args struct {
		currentNodeSets  esv1.NodeSetList
		nodeSetsStatus   status.Status
		requiredCapacity client.AutoscalingPolicyResult
		policy           esv1.AutoscalingPolicySpec
	}
	tests := []struct {
		name            string
		args            args
		want            resources.NodeSetsResources
		wantPolicyState []status.PolicyState
		wantErr         bool
	}{
		{
			name: "Scale both vertically and horizontally to fulfil storage capacity request",
			args: args{
				currentNodeSets: defaultNodeSets,
				nodeSetsStatus: status.Status{AutoscalingPolicyStatuses: []status.AutoscalingPolicyStatus{{
					Name:                   "my-autoscaling-policy",
					NodeSetNodeCount:       []resources.NodeSetNodeCount{{Name: "default", NodeCount: 3}},
					ResourcesSpecification: resources.NodeResources{Requests: map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("3G"), corev1.ResourceStorage: q("1Gi")}}}},
				},
				requiredCapacity: newAutoscalingPolicyResultBuilder().
					nodeMemory("3Gi").nodeStorage("8Gi").
					tierMemory("9Gi").tierStorage("50Gi").
					build(),
				policy: NewAutoscalingSpecBuilder("my-autoscaling-policy").WithNodeCounts(3, 6).WithMemory("3Gi", "4Gi").WithStorage("5Gi", "10Gi").Build(),
			},
			want: resources.NodeSetsResources{
				Name:             "my-autoscaling-policy",
				NodeSetNodeCount: []resources.NodeSetNodeCount{{Name: "default", NodeCount: 5}},
				NodeResources: resources.NodeResources{
					Requests: map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("3Gi"), corev1.ResourceStorage: q("10Gi")},
					Limits:   map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("3Gi")},
				},
			},
		},
		{
			name: "Scale existing nodes vertically",
			args: args{
				currentNodeSets: defaultNodeSets,
				nodeSetsStatus: status.Status{AutoscalingPolicyStatuses: []status.AutoscalingPolicyStatus{{
					Name:                   "my-autoscaling-policy",
					NodeSetNodeCount:       []resources.NodeSetNodeCount{{Name: "default", NodeCount: 3}},
					ResourcesSpecification: resources.NodeResources{Requests: map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("3G"), corev1.ResourceStorage: q("1Gi")}}}},
				},
				requiredCapacity: newAutoscalingPolicyResultBuilder().
					nodeMemory("6G").
					tierMemory("15G").
					build(),
				policy: NewAutoscalingSpecBuilder("my-autoscaling-policy").WithNodeCounts(3, 6).WithMemory("5G", "8G").Build(),
			},
			want: resources.NodeSetsResources{
				Name:             "my-autoscaling-policy",
				NodeSetNodeCount: []resources.NodeSetNodeCount{{Name: "default", NodeCount: 3}},
				NodeResources: resources.NodeResources{
					Requests: map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("6Gi")},
					Limits:   map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("6Gi")},
				},
			},
		},
		{
			name: "Do not scale down storage capacity",
			args: args{
				currentNodeSets: defaultNodeSets,
				nodeSetsStatus: status.Status{AutoscalingPolicyStatuses: []status.AutoscalingPolicyStatus{{
					Name:                   "my-autoscaling-policy",
					NodeSetNodeCount:       []resources.NodeSetNodeCount{{Name: "default", NodeCount: 3}},
					ResourcesSpecification: resources.NodeResources{Requests: map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("4G"), corev1.ResourceStorage: q("10G")}}}},
				},
				requiredCapacity: newAutoscalingPolicyResultBuilder().
					nodeMemory("6G").
					tierMemory("15G").
					nodeStorage("1Gi").
					tierStorage("3Gi").
					build(),
				policy: NewAutoscalingSpecBuilder("my-autoscaling-policy").WithNodeCounts(3, 6).WithMemory("5G", "8G").WithStorage("1G", "20G").Build(),
			},
			want: resources.NodeSetsResources{
				Name:             "my-autoscaling-policy",
				NodeSetNodeCount: []resources.NodeSetNodeCount{{Name: "default", NodeCount: 3}},
				NodeResources: resources.NodeResources{
					Requests: map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("6Gi"), corev1.ResourceStorage: q("10G")},
					Limits:   map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("6Gi")},
				},
			},
		},
		{
			name: "Scale existing nodes vertically up to the tier limit",
			args: args{
				currentNodeSets: defaultNodeSets,
				nodeSetsStatus: status.Status{AutoscalingPolicyStatuses: []status.AutoscalingPolicyStatus{{
					Name:                   "my-autoscaling-policy",
					NodeSetNodeCount:       []resources.NodeSetNodeCount{{Name: "default", NodeCount: 3}},
					ResourcesSpecification: resources.NodeResources{Requests: map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("4G"), corev1.ResourceStorage: q("1Gi")}}}},
				},
				requiredCapacity: newAutoscalingPolicyResultBuilder().
					nodeMemory("6G").
					tierMemory("21G").
					build(),
				policy: NewAutoscalingSpecBuilder("my-autoscaling-policy").WithNodeCounts(3, 6).WithMemory("5G", "8G").Build(),
			},
			want: resources.NodeSetsResources{
				Name:             "my-autoscaling-policy",
				NodeSetNodeCount: []resources.NodeSetNodeCount{{Name: "default", NodeCount: 3}},
				NodeResources: resources.NodeResources{
					Requests: map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("7Gi")},
					Limits:   map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("7Gi")},
				},
			},
		},
		{
			name: "Scale both vertically and horizontally",
			args: args{
				currentNodeSets: defaultNodeSets,
				nodeSetsStatus: status.Status{AutoscalingPolicyStatuses: []status.AutoscalingPolicyStatus{{
					Name:                   "my-autoscaling-policy",
					NodeSetNodeCount:       []resources.NodeSetNodeCount{{Name: "default", NodeCount: 3}},
					ResourcesSpecification: resources.NodeResources{Requests: map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("4G"), corev1.ResourceStorage: q("1Gi")}}}},
				},
				requiredCapacity: newAutoscalingPolicyResultBuilder().
					nodeMemory("6G").
					tierMemory("48G").
					build(),
				policy: NewAutoscalingSpecBuilder("my-autoscaling-policy").WithNodeCounts(3, 6).WithMemory("5G", "8G").Build(),
			},
			want: resources.NodeSetsResources{
				Name:             "my-autoscaling-policy",
				NodeSetNodeCount: []resources.NodeSetNodeCount{{Name: "default", NodeCount: 6}},
				NodeResources: resources.NodeResources{
					Requests: map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("8G")},
					Limits:   map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("8G")},
				},
			},
		},
		{
			name: "Do not exceed node count specified by the user",
			args: args{
				currentNodeSets: defaultNodeSets,
				nodeSetsStatus: status.Status{AutoscalingPolicyStatuses: []status.AutoscalingPolicyStatus{{
					Name:                   "my-autoscaling-policy",
					NodeSetNodeCount:       []resources.NodeSetNodeCount{{Name: "default", NodeCount: 3}},
					ResourcesSpecification: resources.NodeResources{Requests: map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("4G"), corev1.ResourceStorage: q("1Gi")}}}},
				},
				requiredCapacity: newAutoscalingPolicyResultBuilder().
					nodeMemory("6G").
					tierMemory("48G"). // would require 6 nodes, user set a node count limit to 5
					build(),
				policy: NewAutoscalingSpecBuilder("my-autoscaling-policy").WithNodeCounts(3, 5).WithMemory("5G", "8G").Build(),
			},
			want: resources.NodeSetsResources{
				Name:             "my-autoscaling-policy",
				NodeSetNodeCount: []resources.NodeSetNodeCount{{Name: "default", NodeCount: 5}},
				NodeResources: resources.NodeResources{
					Requests: map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("8G")},
					Limits:   map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("8G")},
				},
			},
			wantPolicyState: []status.PolicyState{
				{
					Type:     "HorizontalScalingLimitReached",
					Messages: []string{"Can't provide total required memory 48000000000, max number of nodes is 5, requires 6 nodes"},
				},
			},
		},
		{
			name: "Do not exceed horizontal and vertical limits specified by the user",
			args: args{
				currentNodeSets: defaultNodeSets,
				nodeSetsStatus: status.Status{AutoscalingPolicyStatuses: []status.AutoscalingPolicyStatus{{
					Name:                   "my-autoscaling-policy",
					NodeSetNodeCount:       []resources.NodeSetNodeCount{{Name: "default", NodeCount: 3}},
					ResourcesSpecification: resources.NodeResources{Requests: map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("4G"), corev1.ResourceStorage: q("1Gi")}}}},
				},
				requiredCapacity: newAutoscalingPolicyResultBuilder().
					nodeMemory("8G").  // user set a limit to 5G / node
					tierMemory("48G"). // would require 10
					build(),
				policy: NewAutoscalingSpecBuilder("my-autoscaling-policy").WithNodeCounts(3, 6).WithMemory("5G", "7G").Build(),
			},
			want: resources.NodeSetsResources{
				Name:             "my-autoscaling-policy",
				NodeSetNodeCount: []resources.NodeSetNodeCount{{Name: "default", NodeCount: 6}},
				NodeResources: resources.NodeResources{
					Requests: map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("7G")},
					Limits:   map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("7G")},
				},
			},
			wantPolicyState: []status.PolicyState{
				{
					Type:     "VerticalScalingLimitReached",
					Messages: []string{"Node required memory 8000000000 is greater than max allowed: 7000000000"},
				},
				{
					Type:     "HorizontalScalingLimitReached",
					Messages: []string{"Can't provide total required memory 48000000000, max number of nodes is 6, requires 7 nodes"},
				},
			},
		},
		{
			name: "Do not scale down if all nodes are not observed by Elasticsearch",
			args: args{
				currentNodeSets: defaultNodeSets,
				nodeSetsStatus: status.Status{AutoscalingPolicyStatuses: []status.AutoscalingPolicyStatus{{
					Name:                   "my-autoscaling-policy",
					NodeSetNodeCount:       []resources.NodeSetNodeCount{{Name: "default", NodeCount: 6}},
					ResourcesSpecification: resources.NodeResources{Requests: map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("7G"), corev1.ResourceStorage: q("6G")}}}},
				},
				requiredCapacity: newAutoscalingPolicyResultBuilder().
					nodeStorage("1G").  // biggest shard is 1G
					tierStorage("30G"). // only 5 nodes with 6G of storage each are seen
					observedNodes("default-0", "default-1", "default-2", "default-3", "default-4").
					build(),
				policy: NewAutoscalingSpecBuilder("my-autoscaling-policy").WithNodeCounts(3, 9).WithMemory("5G", "7G").WithStorage("5G", "6G").Build(),
			},
			want: resources.NodeSetsResources{
				Name:             "my-autoscaling-policy",
				NodeSetNodeCount: []resources.NodeSetNodeCount{{Name: "default", NodeCount: 6}}, // do not scale down to 5 nodes
				NodeResources: resources.NodeResources{
					Requests: map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("7G"), corev1.ResourceStorage: q("6G")},
					Limits:   map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("7G")},
				},
			},
			wantPolicyState: []status.PolicyState{},
		},
		{
			name: "Scale down if requested by users even if all nodes are not observed by Elasticsearch",
			args: args{
				currentNodeSets: defaultNodeSets,
				nodeSetsStatus: status.Status{AutoscalingPolicyStatuses: []status.AutoscalingPolicyStatus{{
					Name:                   "my-autoscaling-policy",
					NodeSetNodeCount:       []resources.NodeSetNodeCount{{Name: "default", NodeCount: 6}},
					ResourcesSpecification: resources.NodeResources{Requests: map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("7G"), corev1.ResourceStorage: q("6G")}}}},
				},
				requiredCapacity: newAutoscalingPolicyResultBuilder().
					nodeStorage("1G").  // biggest shard is 1G
					tierStorage("30G"). // only 5 nodes with 6G of storage each are seen
					observedNodes("default-0", "default-1", "default-2", "default-3", "default-4").
					build(),
				policy: NewAutoscalingSpecBuilder("my-autoscaling-policy").WithNodeCounts(3, 5).WithMemory("5G", "7G").WithStorage("5G", "6G").Build(),
			},
			want: resources.NodeSetsResources{
				Name:             "my-autoscaling-policy",
				NodeSetNodeCount: []resources.NodeSetNodeCount{{Name: "default", NodeCount: 5}}, // scale down to 5 nodes as requested by the user
				NodeResources: resources.NodeResources{
					Requests: map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("7G"), corev1.ResourceStorage: q("6G")},
					Limits:   map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("7G")},
				},
			},
			wantPolicyState: []status.PolicyState{},
		},
		{
			name: "Adjust limits",
			args: args{
				currentNodeSets: defaultNodeSets,
				requiredCapacity: newAutoscalingPolicyResultBuilder().
					nodeMemory("6G").tierMemory("15G").
					build(),
				policy: NewAutoscalingSpecBuilder("my-autoscaling-policy").
					WithNodeCounts(3, 6).
					WithMemoryAndRatio("5G", "8G", 2.0).
					Build(),
			},
			want: resources.NodeSetsResources{
				Name:             "my-autoscaling-policy",
				NodeSetNodeCount: []resources.NodeSetNodeCount{{Name: "default", NodeCount: 3}},
				NodeResources: resources.NodeResources{
					Requests: map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("6Gi")},
					Limits:   map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("12Gi")},
				},
			},
		},
		{
			name: "Remove memory limit",
			args: args{
				currentNodeSets: defaultNodeSets,
				requiredCapacity: newAutoscalingPolicyResultBuilder().
					nodeMemory("6G").
					tierMemory("15G").
					build(),
				policy: NewAutoscalingSpecBuilder("my-autoscaling-policy").WithNodeCounts(3, 6).WithMemoryAndRatio("5G", "8G", 0.0).Build(),
			},
			want: resources.NodeSetsResources{
				Name:             "my-autoscaling-policy",
				NodeSetNodeCount: []resources.NodeSetNodeCount{{Name: "default", NodeCount: 3}},
				NodeResources: resources.NodeResources{
					Requests: map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: q("6Gi")},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := Context{
				Log:                      logTest,
				AutoscalingSpec:          tt.args.policy,
				NodeSets:                 tt.args.currentNodeSets,
				CurrentAutoscalingStatus: tt.args.nodeSetsStatus,
				AutoscalingPolicyResult:  tt.args.requiredCapacity,
				StatusBuilder:            status.NewAutoscalingStatusBuilder(),
			}
			if got := ctx.GetResources(); !equality.Semantic.DeepEqual(got, tt.want) {
				t.Errorf("autoscaler.GetResources() = %v, want %v", got, tt.want)
			}
			gotStatus := ctx.StatusBuilder.Build()
			assert.ElementsMatch(t, getPolicyStates(gotStatus, "my-autoscaling-policy"), tt.wantPolicyState)
		})
	}
}

func getPolicyStates(status status.Status, policyName string) []status.PolicyState {
	for _, state := range status.AutoscalingPolicyStatuses {
		if state.Name == policyName {
			return state.PolicyStates
		}
	}
	return nil
}

// - AutoscalingSpec builder

type AutoscalingSpecBuilder struct {
	name                       string
	nodeCountMin, nodeCountMax int32
	cpu, memory, storage       *esv1.QuantityRange
}

func NewAutoscalingSpecBuilder(name string) *AutoscalingSpecBuilder {
	return &AutoscalingSpecBuilder{name: name}
}

func (asb *AutoscalingSpecBuilder) WithNodeCounts(min, max int) *AutoscalingSpecBuilder {
	asb.nodeCountMin = int32(min)
	asb.nodeCountMax = int32(max)
	return asb
}

func (asb *AutoscalingSpecBuilder) WithMemory(min, max string) *AutoscalingSpecBuilder {
	asb.memory = &esv1.QuantityRange{
		Min: resource.MustParse(min),
		Max: resource.MustParse(max),
	}
	return asb
}

func (asb *AutoscalingSpecBuilder) WithMemoryAndRatio(min, max string, ratio float64) *AutoscalingSpecBuilder {
	asb.memory = &esv1.QuantityRange{
		Min:                   resource.MustParse(min),
		Max:                   resource.MustParse(max),
		RequestsToLimitsRatio: &ratio,
	}
	return asb
}

func (asb *AutoscalingSpecBuilder) WithStorage(min, max string) *AutoscalingSpecBuilder {
	asb.storage = &esv1.QuantityRange{
		Min: resource.MustParse(min),
		Max: resource.MustParse(max),
	}
	return asb
}

func (asb *AutoscalingSpecBuilder) WithCPU(min, max string) *AutoscalingSpecBuilder {
	asb.cpu = &esv1.QuantityRange{
		Min: resource.MustParse(min),
		Max: resource.MustParse(max),
	}
	return asb
}

func (asb *AutoscalingSpecBuilder) WithCPUAndRatio(min, max string, ratio float64) *AutoscalingSpecBuilder {
	asb.cpu = &esv1.QuantityRange{
		Min:                   resource.MustParse(min),
		Max:                   resource.MustParse(max),
		RequestsToLimitsRatio: &ratio,
	}
	return asb
}

func (asb *AutoscalingSpecBuilder) Build() esv1.AutoscalingPolicySpec {
	return esv1.AutoscalingPolicySpec{
		NamedAutoscalingPolicy: esv1.NamedAutoscalingPolicy{
			Name: asb.name,
		},
		AutoscalingResources: esv1.AutoscalingResources{
			CPURange:     asb.cpu,
			MemoryRange:  asb.memory,
			StorageRange: asb.storage,
			NodeCountRange: esv1.CountRange{
				Min: asb.nodeCountMin,
				Max: asb.nodeCountMax,
			},
		},
	}
}

// - PolicyCapacityInfo builder

type autoscalingPolicyResultBuilder struct {
	client.AutoscalingPolicyResult
}

func newAutoscalingPolicyResultBuilder() *autoscalingPolicyResultBuilder {
	return &autoscalingPolicyResultBuilder{}
}

func (rcb *autoscalingPolicyResultBuilder) build() client.AutoscalingPolicyResult {
	return rcb.AutoscalingPolicyResult
}

func (rcb *autoscalingPolicyResultBuilder) nodeMemory(m string) *autoscalingPolicyResultBuilder {
	rcb.RequiredCapacity.Node.Memory = ptr(value(m))
	return rcb
}

func (rcb *autoscalingPolicyResultBuilder) tierMemory(m string) *autoscalingPolicyResultBuilder {
	rcb.RequiredCapacity.Total.Memory = ptr(value(m))
	return rcb
}

func (rcb *autoscalingPolicyResultBuilder) nodeStorage(m string) *autoscalingPolicyResultBuilder {
	rcb.RequiredCapacity.Node.Storage = ptr(value(m))
	return rcb
}

func (rcb *autoscalingPolicyResultBuilder) tierStorage(m string) *autoscalingPolicyResultBuilder {
	rcb.RequiredCapacity.Total.Storage = ptr(value(m))
	return rcb
}

func (rcb *autoscalingPolicyResultBuilder) observedNodes(nodes ...string) *autoscalingPolicyResultBuilder {
	rcb.CurrentNodes = make([]client.AutoscalingNodeInfo, len(nodes))
	for i := range nodes {
		rcb.CurrentNodes[i] = client.AutoscalingNodeInfo{Name: nodes[i]}
	}
	return rcb
}

func ptr(q int64) *client.AutoscalingCapacity {
	v := client.AutoscalingCapacity(q)
	return &v
}

func value(v string) int64 {
	q := resource.MustParse(v)
	return q.Value()
}
