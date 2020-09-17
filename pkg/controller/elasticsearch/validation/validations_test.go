// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package validation

import (
	"testing"

	"k8s.io/utils/pointer"

	storagev1 "k8s.io/api/storage/v1"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/elastic/cloud-on-k8s/pkg/utils/k8s"

	esv1 "github.com/elastic/cloud-on-k8s/pkg/apis/elasticsearch/v1"
	v1 "github.com/elastic/cloud-on-k8s/pkg/apis/elasticsearch/v1"

	commonv1 "github.com/elastic/cloud-on-k8s/pkg/apis/common/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_checkNodeSetNameUniqueness(t *testing.T) {
	type args struct {
		name         string
		es           *v1.Elasticsearch
		expectErrors bool
	}
	tests := []args{
		{
			name: "several duplicate nodeSets",
			es: &v1.Elasticsearch{
				Spec: v1.ElasticsearchSpec{
					Version: "7.4.0",
					NodeSets: []v1.NodeSet{
						{Name: "foo", Count: 1},
						{Name: "foo", Count: 1},
						{Name: "bar", Count: 1},
						{Name: "bar", Count: 1},
					},
				},
			},
			expectErrors: true,
		},
		{
			name: "good spec with 1 nodeSet",
			es: &v1.Elasticsearch{
				Spec: v1.ElasticsearchSpec{
					Version:  "7.4.0",
					NodeSets: []v1.NodeSet{{Name: "foo", Count: 1}},
				},
			},
			expectErrors: false,
		},
		{
			name: "good spec with 2 nodeSets",
			es: &v1.Elasticsearch{
				TypeMeta: metav1.TypeMeta{APIVersion: "elasticsearch.k8s.elastic.co/v1"},
				Spec: v1.ElasticsearchSpec{
					Version:  "7.4.0",
					NodeSets: []v1.NodeSet{{Name: "foo", Count: 1}, {Name: "bar", Count: 1}},
				},
			},
			expectErrors: false,
		},
		{
			name: "duplicate nodeSet",
			es: &v1.Elasticsearch{
				TypeMeta: metav1.TypeMeta{APIVersion: "elasticsearch.k8s.elastic.co/v1"},
				Spec: v1.ElasticsearchSpec{
					Version:  "7.4.0",
					NodeSets: []v1.NodeSet{{Name: "foo", Count: 1}, {Name: "foo", Count: 1}},
				},
			},
			expectErrors: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := checkNodeSetNameUniqueness(tt.es)
			actualErrors := len(actual) > 0

			if tt.expectErrors != actualErrors {
				t.Errorf("failed checkNodeSetNameUniqueness(). Name: %v, actual %v, wanted: %v, value: %v", tt.name, actual, tt.expectErrors, tt.es.Spec.NodeSets)
			}
		})
	}
}

func Test_hasCorrectNodeRoles(t *testing.T) {
	type m map[string]interface{}

	esWithRoles := func(version string, count int32, nodeSetRoles ...m) esv1.Elasticsearch {
		x := es(version)
		for _, nsc := range nodeSetRoles {
			data := nsc
			var cfg *commonv1.Config
			if data != nil {
				cfg = &commonv1.Config{Data: data}
			}

			x.Spec.NodeSets = append(x.Spec.NodeSets, v1.NodeSet{
				Count:  count,
				Config: cfg,
			})
		}

		return x
	}

	tests := []struct {
		name         string
		es           esv1.Elasticsearch
		expectErrors bool
	}{
		{
			name:         "no topology",
			es:           esWithRoles("6.8.0", 1),
			expectErrors: true,
		},
		{
			name:         "one nodeset with no config",
			es:           esWithRoles("7.6.0", 1, nil),
			expectErrors: false,
		},
		{
			name:         "no master defined (node attributes)",
			es:           esWithRoles("7.6.0", 1, m{v1.NodeMaster: "false", v1.NodeData: "true"}, m{v1.NodeMaster: "true", v1.NodeVotingOnly: "true"}),
			expectErrors: true,
		},
		{
			name:         "no master defined (node roles)",
			es:           esWithRoles("7.9.0", 1, m{v1.NodeRoles: []string{v1.DataRole}}, m{v1.NodeRoles: []string{v1.MasterRole, v1.VotingOnlyRole}}),
			expectErrors: true,
		},
		{
			name:         "zero master nodes (node attributes)",
			es:           esWithRoles("7.6.0", 0, m{v1.NodeMaster: "true", v1.NodeData: "true"}, m{v1.NodeData: "true"}),
			expectErrors: true,
		},
		{
			name:         "zero master nodes (node roles)",
			es:           esWithRoles("7.9.0", 0, m{v1.NodeRoles: []string{v1.MasterRole, v1.DataRole}}, m{v1.NodeRoles: []string{v1.DataRole}}),
			expectErrors: true,
		},
		{
			name:         "mixed node attributes and node roles",
			es:           esWithRoles("7.9.0", 1, m{v1.NodeMaster: "true", v1.NodeRoles: []string{v1.DataRole}}, m{v1.NodeRoles: []string{v1.DataRole, v1.TransformRole}}),
			expectErrors: true,
		},
		{
			name:         "node roles on older version",
			es:           esWithRoles("7.6.0", 1, m{v1.NodeRoles: []string{v1.MasterRole}}, m{v1.NodeRoles: []string{v1.DataRole}}),
			expectErrors: true,
		},
		{
			name: "valid configuration (node attributes)",
			es:   esWithRoles("7.6.0", 3, m{v1.NodeMaster: "true", v1.NodeData: "true"}, m{v1.NodeData: "true"}),
		},
		{
			name: "valid configuration (node roles)",
			es:   esWithRoles("7.9.0", 3, m{v1.NodeRoles: []string{v1.MasterRole, v1.DataRole}}, m{v1.NodeRoles: []string{v1.DataRole}}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasCorrectNodeRoles(tt.es)
			hasErrors := len(result) > 0
			if tt.expectErrors != hasErrors {
				t.Errorf("expectedErrors=%t hasErrors=%t result=%+v", tt.expectErrors, hasErrors, result)
			}
		})
	}
}

func Test_supportedVersion(t *testing.T) {
	tests := []struct {
		name         string
		es           esv1.Elasticsearch
		expectErrors bool
	}{
		{
			name: "unsupported minor version should fail",
			es:   es("6.0.0"),

			expectErrors: true,
		},
		{
			name:         "unsupported major should fail",
			es:           es("1.0.0"),
			expectErrors: true,
		},
		{
			name:         "supported OK",
			es:           es("6.8.0"),
			expectErrors: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := supportedVersion(tt.es)
			actualErrors := len(actual) > 0
			if tt.expectErrors != actualErrors {
				t.Errorf("failed supportedVersion(). Name: %v, actual %v, wanted: %v, value: %v", tt.name, actual, tt.expectErrors, tt.es.Spec.Version)
			}
		})
	}
}

func Test_validName(t *testing.T) {
	tests := []struct {
		name         string
		es           esv1.Elasticsearch
		expectErrors bool
	}{
		{
			name: "name length too long",
			es: esv1.Elasticsearch{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "that-is-a-very-long-name-with-37chars",
				},
			},
			expectErrors: true,
		},
		{
			name: "name length OK",
			es: esv1.Elasticsearch{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "that-is-a-very-long-name-with-36char",
				},
			},
			expectErrors: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := validName(tt.es)
			actualErrors := len(actual) > 0
			if tt.expectErrors != actualErrors {
				t.Errorf("failed validName(). Name: %v, actual %v, wanted: %v, value: %v", tt.name, actual, tt.expectErrors, tt.es.Name)
			}
		})
	}
}

func Test_validSanIP(t *testing.T) {
	validIP := "3.4.5.6"
	validIP2 := "192.168.12.13"
	validIPv6 := "2001:db8:0:85a3:0:0:ac1f:8001"
	invalidIP := "notanip"

	tests := []struct {
		name         string
		es           esv1.Elasticsearch
		expectErrors bool
	}{
		{
			name: "no SAN IP: OK",
			es: esv1.Elasticsearch{
				Spec: esv1.ElasticsearchSpec{},
			},
			expectErrors: false,
		},
		{
			name: "valid SAN IPs: OK",
			es: esv1.Elasticsearch{
				Spec: v1.ElasticsearchSpec{
					HTTP: commonv1.HTTPConfig{
						TLS: commonv1.TLSOptions{
							SelfSignedCertificate: &commonv1.SelfSignedCertificate{
								SubjectAlternativeNames: []commonv1.SubjectAlternativeName{
									{
										IP: validIP,
									},
									{
										IP: validIP2,
									},
									{
										IP: validIPv6,
									},
								},
							},
						},
					},
				},
			},
			expectErrors: false,
		},
		{
			name: "invalid SAN IPs: NOT OK",
			es: esv1.Elasticsearch{
				Spec: v1.ElasticsearchSpec{
					HTTP: commonv1.HTTPConfig{
						TLS: commonv1.TLSOptions{
							SelfSignedCertificate: &commonv1.SelfSignedCertificate{
								SubjectAlternativeNames: []commonv1.SubjectAlternativeName{
									{
										IP: invalidIP,
									},
									{
										IP: validIP2,
									},
								},
							},
						},
					},
				},
			},
			expectErrors: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := validSanIP(tt.es)
			actualErrors := len(actual) > 0
			if tt.expectErrors != actualErrors {
				t.Errorf("failed validSanIP(). Name: %v, actual %v, wanted: %v, value: %v", tt.name, actual, tt.expectErrors, tt.es.Spec)
			}
		})
	}
}

func TestValidation_noDowngrades(t *testing.T) {
	tests := []struct {
		name         string
		current      esv1.Elasticsearch
		proposed     esv1.Elasticsearch
		expectErrors bool
	}{
		{
			name:         "prevent downgrade",
			current:      es("2.0.0"),
			proposed:     es("1.0.0"),
			expectErrors: true,
		},
		{
			name:         "allow upgrades",
			current:      es("1.0.0"),
			proposed:     es("1.2.0"),
			expectErrors: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := noDowngrades(tt.current, tt.proposed, nil)
			actualErrors := len(actual) > 0
			if tt.expectErrors != actualErrors {
				t.Errorf("failed noDowngrades(). Name: %v, actual %v, wanted: %v, value: %v", tt.name, actual, tt.expectErrors, tt.proposed)
			}
		})
	}
}

func Test_validUpgradePath(t *testing.T) {
	tests := []struct {
		name         string
		current      esv1.Elasticsearch
		proposed     esv1.Elasticsearch
		expectErrors bool
	}{
		{
			name:     "unsupported version rejected",
			current:  es("1.0.0"),
			proposed: es("2.0.0"),

			expectErrors: true,
		},
		{
			name:         "too old version rejected",
			current:      es("6.5.0"),
			proposed:     es("7.0.0"),
			expectErrors: true,
		},
		{
			name:         "too new rejected",
			current:      es("7.0.0"),
			proposed:     es("6.5.0"),
			expectErrors: true,
		},
		{
			name:         "in range accepted",
			current:      es("6.8.0"),
			proposed:     es("7.1.0"),
			expectErrors: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := validUpgradePath(tt.current, tt.proposed, nil)
			actualErrors := len(actual) > 0
			if tt.expectErrors != actualErrors {
				t.Errorf("failed validUpgradePath(). Name: %v, actual %v, wanted: %v, value: %v", tt.name, actual, tt.expectErrors, tt.proposed)
			}
		})
	}
}

func Test_noUnknownFields(t *testing.T) {
	GetEsWithLastApplied := func(lastApplied string) v1.Elasticsearch {
		return v1.Elasticsearch{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					corev1.LastAppliedConfigAnnotation: lastApplied,
				},
			},
		}
	}

	tests := []struct {
		name         string
		es           v1.Elasticsearch
		errorOnField string
	}{
		{
			name: "good annotation",
			es: GetEsWithLastApplied(
				`{"apiVersion":"elasticsearch.k8s.elastic.co/v1","kind":"Elasticsearch"` +
					`,"metadata":{"annotations":{},"name":"quickstart","namespace":"default"},` +
					`"spec":{"nodeSets":[{"config":{"node.store.allow_mmap":false},"count":1,` +
					`"name":"default"}],"version":"7.5.1"}}`),
		},
		{
			name: "no annotation",
			es:   v1.Elasticsearch{},
		},
		{
			name: "bad annotation",
			es: GetEsWithLastApplied(
				`{"apiVersion":"elasticsearch.k8s.elastic.co/v1","kind":"Elasticsearch"` +
					`,"metadata":{"annotations":{},"name":"quickstart","namespace":"default"},` +
					`"spec":{"nodeSets":[{"config":{"node.store.allow_mmap":false},"count":1,` +
					`"name":"default","wrongthing":true}],"version":"7.5.1"}}`),
			errorOnField: "wrongthing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := noUnknownFields(tt.es)
			actualErrors := len(actual) > 0
			expectErrors := tt.errorOnField != ""
			if expectErrors != actualErrors || (actualErrors && actual[0].Field != tt.errorOnField) {
				t.Errorf(
					"failed NoUnknownFields(). Name: %v, actual %v, wanted error on field: %v, es value: %v",
					tt.name,
					actual,
					tt.errorOnField,
					tt.es)
			}
		})
	}
}

// es returns an es fixture at a given version
func es(v string) esv1.Elasticsearch {
	return v1.Elasticsearch{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "foo",
		},
		Spec: v1.ElasticsearchSpec{Version: v},
	}
}

func Test_pvcModification(t *testing.T) {
	scExpansion := storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "sample-sc-expansion"}, AllowVolumeExpansion: pointer.BoolPtr(true)}
	scNoExpansion := storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "sample-sc-no-expansion"}}
	es1Gi := esv1.Elasticsearch{
		Spec: esv1.ElasticsearchSpec{
			Version: "7.2.0",
			NodeSets: []esv1.NodeSet{
				{
					Name: "master",
					VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "elasticsearch-data",
							},
							Spec: corev1.PersistentVolumeClaimSpec{
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("1Gi"),
									},
								},
								StorageClassName: pointer.StringPtr(scExpansion.Name),
							},
						},
					},
				},
			},
		},
	}

	type args struct {
		current  func() v1.Elasticsearch
		proposed func() v1.Elasticsearch
		client   k8s.Client
	}
	tests := []struct {
		name         string
		args         args
		expectErrors bool
	}{
		{
			name: "same claims: allow",
			args: args{
				current:  func() esv1.Elasticsearch { return *es1Gi.DeepCopy() },
				proposed: func() esv1.Elasticsearch { return *es1Gi.DeepCopy() },
				client:   k8s.WrappedFakeClient(&scExpansion),
			},
			expectErrors: false,
		},
		{
			name: "same claim with size expressed differently (1024Mi vs. 1Gi): allow",
			args: args{
				current: func() esv1.Elasticsearch { return *es1Gi.DeepCopy() },
				proposed: func() esv1.Elasticsearch {
					resized := *es1Gi.DeepCopy()
					resized.Spec.NodeSets[0].VolumeClaimTemplates[0].Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("1024Mi")
					return resized
				},
				client: k8s.WrappedFakeClient(&scExpansion),
			},
			expectErrors: false,
		},
		{
			name: "volume expansion: allow",
			args: args{
				current: func() esv1.Elasticsearch { return *es1Gi.DeepCopy() },
				proposed: func() esv1.Elasticsearch {
					resized := *es1Gi.DeepCopy()
					resized.Spec.NodeSets[0].VolumeClaimTemplates[0].Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("2Gi")
					return resized
				},
				client: k8s.WrappedFakeClient(&scExpansion),
			},
			expectErrors: false,
		},
		{
			name: "new nodeSet: allow",
			args: args{
				current: func() esv1.Elasticsearch { return *es1Gi.DeepCopy() },
				proposed: func() esv1.Elasticsearch {
					withNewNodeSet := *es1Gi.DeepCopy()
					nodeSet := *es1Gi.Spec.NodeSets[0].DeepCopy()
					nodeSet.Name = "another-nodeset"
					withNewNodeSet.Spec.NodeSets = append(withNewNodeSet.Spec.NodeSets, nodeSet)
					return withNewNodeSet
				},
				client: k8s.WrappedFakeClient(&scExpansion),
			},
			expectErrors: false,
		},
		{
			name: "volume expansion with incompatible storage class: disallow",
			args: args{
				current: func() esv1.Elasticsearch {
					patchedSc := *es1Gi.DeepCopy()
					patchedSc.Spec.NodeSets[0].VolumeClaimTemplates[0].Spec.StorageClassName = pointer.StringPtr(scNoExpansion.Name)
					return patchedSc
				},
				proposed: func() esv1.Elasticsearch {
					resized := *es1Gi.DeepCopy()
					resized.Spec.NodeSets[0].VolumeClaimTemplates[0].Spec.StorageClassName = pointer.StringPtr(scNoExpansion.Name)
					resized.Spec.NodeSets[0].VolumeClaimTemplates[0].Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("2Gi")
					return resized
				},
				client: k8s.WrappedFakeClient(&scNoExpansion),
			},
			expectErrors: true,
		},
		{
			name: "modified storage class name: disallow",
			args: args{
				current: func() esv1.Elasticsearch { return *es1Gi.DeepCopy() },
				proposed: func() esv1.Elasticsearch {
					differentSc := *es1Gi.DeepCopy()
					differentSc.Spec.NodeSets[0].VolumeClaimTemplates[0].Spec.StorageClassName = pointer.StringPtr(scNoExpansion.Name)
					return differentSc
				},
				client: k8s.WrappedFakeClient(&scNoExpansion),
			},
			expectErrors: true,
		},
		{
			name: "removed claim: disallow",
			args: args{
				current: func() esv1.Elasticsearch { return *es1Gi.DeepCopy() },
				proposed: func() esv1.Elasticsearch {
					noClaim := *es1Gi.DeepCopy()
					noClaim.Spec.NodeSets[0].VolumeClaimTemplates = nil
					return noClaim
				},
				client: k8s.WrappedFakeClient(&scNoExpansion),
			},
			expectErrors: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pvcModification(tt.args.current(), tt.args.proposed(), tt.args.client)
			actualErrors := len(got) > 0
			if tt.expectErrors != actualErrors {
				t.Errorf("failed pvcModification(). Name: %v, actual %v, wanted: %v, value: %v", tt.name, actualErrors, tt.expectErrors, got)
			}
		})
	}
}
