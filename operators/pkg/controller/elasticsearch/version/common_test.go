// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package version

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/elastic/cloud-on-k8s/operators/pkg/apis/common/v1alpha1"
	"github.com/elastic/cloud-on-k8s/operators/pkg/apis/elasticsearch/v1alpha1"
	"github.com/elastic/cloud-on-k8s/operators/pkg/controller/common/volume"
	"github.com/elastic/cloud-on-k8s/operators/pkg/controller/elasticsearch/pod"
	"github.com/elastic/cloud-on-k8s/operators/pkg/controller/elasticsearch/processmanager"
	"github.com/elastic/cloud-on-k8s/operators/pkg/controller/elasticsearch/settings"
)

func Test_quantityToMegabytes(t *testing.T) {
	type args struct {
		q resource.Quantity
	}
	tests := []struct {
		name string
		args args
		want int
	}{
		{name: "simple", args: args{q: resource.MustParse("2Gi")}, want: 2 * 1024},
		{name: "large", args: args{q: resource.MustParse("9Ti")}, want: 9 * 1024 * 1024},
		{name: "small", args: args{q: resource.MustParse("0.25Gi")}, want: 256},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quantityToMegabytes(tt.args.q); got != tt.want {
				t.Errorf("quantityToMegabytes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewPod(t *testing.T) {
	esMeta := metav1.ObjectMeta{
		Namespace: "ns",
		Name:      "name",
	}
	podSpec := corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name: "container1",
			},
		},
		Subdomain: esMeta.Namespace,
		Hostname:  esMeta.Name,
	}
	// configurePodSpec is a helper method to set attributes on a pod spec without modifying the original
	configurePodSpec := func(spec corev1.PodSpec, configure func(*corev1.PodSpec)) corev1.PodSpec {
		s := spec.DeepCopy()
		configure(s)
		return *s
	}

	tests := []struct {
		name       string
		es         v1alpha1.Elasticsearch
		podSpecCtx pod.PodSpecContext
		want       corev1.Pod
	}{
		{
			name: "no podTemplate",
			es: v1alpha1.Elasticsearch{
				ObjectMeta: esMeta,
				Spec: v1alpha1.ElasticsearchSpec{
					Version: "7.1.0",
				},
			},
			podSpecCtx: pod.PodSpecContext{
				PodSpec: podSpec,
			},
			want: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: esMeta.Namespace,
					Name:      esMeta.Name,
				},
				Spec: podSpec,
			},
		},
		{
			name: "with podTemplate: should propagate annotations and subdomain",
			es: v1alpha1.Elasticsearch{
				ObjectMeta: esMeta,
				Spec: v1alpha1.ElasticsearchSpec{
					Version: "7.1.0",
				},
			},
			podSpecCtx: pod.PodSpecContext{
				PodSpec: configurePodSpec(podSpec, func(spec *corev1.PodSpec) {
					spec.Subdomain = "my-subdomain"
				}),
				NodeSpec: v1alpha1.NodeSpec{
					PodTemplate: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"annotation1": "foo",
								"annotation2": "bar",
							},
						},
					},
				},
			},
			want: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: esMeta.Namespace,
					Name:      esMeta.Name,
					Annotations: map[string]string{
						"annotation1": "foo",
						"annotation2": "bar",
					},
				},
				Spec: configurePodSpec(podSpec, func(spec *corev1.PodSpec) {
					spec.Subdomain = "my-subdomain"
				}),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewPod(tt.es, tt.podSpecCtx)
			require.NoError(t, err)
			// since the name is random, don't test its equality and inject it to the expected output
			tt.want.Name = got.Name

			require.Equal(t, tt.want, got)
		})
	}
}

func Test_podSpec(t *testing.T) {
	// this test focuses on testing user-provided pod template overrides
	// setup mocks for env vars func, es config func and init-containers func
	newEnvVarsFn := func(p pod.NewPodSpecParams, certs, creds, keystore volume.SecretVolume) []corev1.EnvVar {
		return []corev1.EnvVar{
			{
				Name:  "var1",
				Value: "value1",
			},
			{
				Name:  "var2",
				Value: "value2",
			},
		}
	}
	newESConfigFn := func(clusterName string, config commonv1alpha1.Config) (settings.CanonicalConfig, error) {
		return settings.NewCanonicalConfig(), nil
	}
	newInitContainersFn := func(elasticsearchImage string, operatorImage string, setVMMaxMapCount *bool, nodeCertificatesVolume volume.SecretVolume, clusterName string) ([]corev1.Container, error) {
		return []corev1.Container{
			{
				Name: "init-container1",
			},
			{
				Name: "init-container2",
			},
		}, nil
	}
	varFalse := false
	varTrue := true
	varInt64 := int64(12)
	es71 := v1alpha1.Elasticsearch{
		Spec: v1alpha1.ElasticsearchSpec{
			Version: "7.1.0",
		},
	}

	tests := []struct {
		name       string
		params     pod.NewPodSpecParams
		assertions func(t *testing.T, specCtx pod.PodSpecContext)
	}{
		{
			name: "no podTemplate: default happy path",
			params: pod.NewPodSpecParams{
				Elasticsearch: es71,
			},
			assertions: func(t *testing.T, specCtx pod.PodSpecContext) {
				podSpec := specCtx.PodSpec
				require.Equal(t, fmt.Sprintf("%s:%s", pod.DefaultImageRepository, "7.1.0"), podSpec.Containers[0].Image)
				require.Equal(t, pod.DefaultTerminationGracePeriodSeconds, *podSpec.TerminationGracePeriodSeconds)
				require.Equal(t, &varFalse, podSpec.AutomountServiceAccountToken)
				require.NotEmpty(t, podSpec.Volumes)
				require.Len(t, podSpec.InitContainers, 2)
				require.Len(t, podSpec.Containers, 1)
				esContainer := podSpec.Containers[0]
				require.NotEmpty(t, esContainer.VolumeMounts)
				require.Len(t, esContainer.Env, 2)
				require.Nil(t, esContainer.Resources.Requests)
				require.Equal(t, pod.DefaultContainerPorts, esContainer.Ports)
				require.Equal(t, pod.NewReadinessProbe(), esContainer.ReadinessProbe)
				require.Equal(t, []string{processmanager.CommandPath}, esContainer.Command)
			},
		},
		{
			name: "custom image",
			params: pod.NewPodSpecParams{
				Elasticsearch: v1alpha1.Elasticsearch{
					Spec: v1alpha1.ElasticsearchSpec{
						Image:   "customImageName",
						Version: "7.1.0",
					},
				},
			},
			assertions: func(t *testing.T, specCtx pod.PodSpecContext) {
				require.Equal(t, "customImageName", specCtx.PodSpec.Containers[0].Image)
			},
		},
		{
			name: "custom termination grace period & automount sa token",
			params: pod.NewPodSpecParams{
				Elasticsearch: es71,
				NodeSpec: v1alpha1.NodeSpec{
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							TerminationGracePeriodSeconds: &varInt64,
							AutomountServiceAccountToken:  &varTrue,
						},
					},
				},
			},
			assertions: func(t *testing.T, specCtx pod.PodSpecContext) {
				require.Equal(t, &varInt64, specCtx.PodSpec.TerminationGracePeriodSeconds)
				require.Equal(t, &varTrue, specCtx.PodSpec.AutomountServiceAccountToken)
			},
		},
		{
			name: "user-provided volumes & volume mounts",
			params: pod.NewPodSpecParams{
				Elasticsearch: es71,
				NodeSpec: v1alpha1.NodeSpec{
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Volumes: []corev1.Volume{
								{
									Name: "user-volume-1",
								},
								{
									Name: "user-volume-2",
								},
							},
							Containers: []corev1.Container{
								{
									Name: v1alpha1.ElasticsearchContainerName,
									VolumeMounts: []corev1.VolumeMount{
										{
											Name: "user-volume-mount-1",
										},
										{
											Name: "user-volume-mount-2",
										},
									},
								},
							},
						},
					},
				},
			},
			assertions: func(t *testing.T, specCtx pod.PodSpecContext) {
				podSpec := specCtx.PodSpec
				require.True(t, len(podSpec.Volumes) > 1)
				foundUserVolumes := 0
				for _, v := range podSpec.Volumes {
					if v.Name == "user-volume-1" || v.Name == "user-volume-2" {
						foundUserVolumes++
					}
				}
				require.Equal(t, 2, foundUserVolumes)
				foundUserVolumeMounts := 0
				for _, v := range podSpec.Containers[0].VolumeMounts {
					if v.Name == "user-volume-mount-1" || v.Name == "user-volume-mount-2" {
						foundUserVolumeMounts++
					}
				}
				require.Equal(t, 2, foundUserVolumeMounts)
			},
		},
		{
			name: "user-provided init containers",
			params: pod.NewPodSpecParams{
				Elasticsearch: es71,
				NodeSpec: v1alpha1.NodeSpec{
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							InitContainers: []corev1.Container{
								{
									Name:  "user-init-container-1",
									Image: "my-custom-image",
								},
								{
									Name: "user-init-container-2",
									VolumeMounts: []corev1.VolumeMount{{
										Name:      "foo",
										MountPath: "/foo",
									}},
								},
							},
						},
					},
				},
			},
			assertions: func(t *testing.T, specCtx pod.PodSpecContext) {
				podSpec := specCtx.PodSpec
				require.Equal(t, []corev1.Container{
					{
						Name: "init-container1",
					},
					{
						Name: "init-container2",
					},
					{
						Name:         "user-init-container-1",
						Image:        "my-custom-image",
						VolumeMounts: podSpec.Containers[0].VolumeMounts,
					},
					{
						Name:  "user-init-container-2",
						Image: podSpec.Containers[0].Image,
						VolumeMounts: append(
							[]corev1.VolumeMount{{
								Name:      "foo",
								MountPath: "/foo",
							}},
							podSpec.Containers[0].VolumeMounts...,
						),
					},
				}, podSpec.InitContainers)
			},
		},
		{
			name: "user-provided environment",
			params: pod.NewPodSpecParams{
				Elasticsearch: es71,
				NodeSpec: v1alpha1.NodeSpec{
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: v1alpha1.ElasticsearchContainerName,
									Env: []corev1.EnvVar{
										{
											Name:  "user-env-1",
											Value: "user-env-1-value",
										},
										{
											Name:  "user-env-2",
											Value: "user-env-2-value",
										},
									},
								},
							},
						},
					},
				},
			},
			assertions: func(t *testing.T, specCtx pod.PodSpecContext) {
				require.Equal(t, []corev1.EnvVar{
					{
						Name:  "user-env-1",
						Value: "user-env-1-value",
					},
					{
						Name:  "user-env-2",
						Value: "user-env-2-value",
					},
					{
						Name:  "var1",
						Value: "value1",
					},
					{
						Name:  "var2",
						Value: "value2",
					},
				}, specCtx.PodSpec.Containers[0].Env)
			},
		},
		{
			name: "user-provided environment overrides",
			params: pod.NewPodSpecParams{
				Elasticsearch: es71,
				NodeSpec: v1alpha1.NodeSpec{
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: v1alpha1.ElasticsearchContainerName,
									Env: []corev1.EnvVar{
										{
											Name:  "var1",
											Value: "user-overridden-var-1",
										},
										{
											Name:  "user-env-2",
											Value: "user-env-2-value",
										},
									},
								},
							},
						},
					},
				},
			},
			assertions: func(t *testing.T, specCtx pod.PodSpecContext) {
				require.Equal(t, []corev1.EnvVar{
					{
						Name:  "user-env-2",
						Value: "user-env-2-value",
					},
					{
						Name:  "var1",
						Value: "user-overridden-var-1",
					},
					{
						Name:  "var2",
						Value: "value2",
					},
				}, specCtx.PodSpec.Containers[0].Env)
			},
		},
		{
			name: "default affinity",
			params: pod.NewPodSpecParams{
				Elasticsearch: v1alpha1.Elasticsearch{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-cluster",
					},
					Spec: v1alpha1.ElasticsearchSpec{
						Version: "7.1.0",
					},
				},
			},
			assertions: func(t *testing.T, specCtx pod.PodSpecContext) {
				require.Equal(t, pod.DefaultAffinity("my-cluster"), specCtx.PodSpec.Affinity)
			},
		},
		{
			name: "custom affinity",
			params: pod.NewPodSpecParams{
				Elasticsearch: v1alpha1.Elasticsearch{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-cluster",
					},
					Spec: v1alpha1.ElasticsearchSpec{
						Version: "7.1.0",
					},
				},
				NodeSpec: v1alpha1.NodeSpec{
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Affinity: &corev1.Affinity{},
						},
					},
				},
			},
			assertions: func(t *testing.T, specCtx pod.PodSpecContext) {
				require.Equal(t, &corev1.Affinity{}, specCtx.PodSpec.Affinity)
			},
		},
		{
			name: "user-provided labels",
			params: pod.NewPodSpecParams{
				Elasticsearch: es71,
				NodeSpec: v1alpha1.NodeSpec{
					PodTemplate: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"a": "b",
								"c": "d",
							},
						},
					},
				},
			},
			assertions: func(t *testing.T, specCtx pod.PodSpecContext) {
				require.Equal(t, map[string]string{
					"a":                          "b",
					"c":                          "d",
					"common.k8s.elastic.co/type": "elasticsearch",
					"elasticsearch.k8s.elastic.co/cluster-name": "",
					"elasticsearch.k8s.elastic.co/node-data":    "true",
					"elasticsearch.k8s.elastic.co/node-ingest":  "true",
					"elasticsearch.k8s.elastic.co/node-master":  "true",
					"elasticsearch.k8s.elastic.co/node-ml":      "true",
					"elasticsearch.k8s.elastic.co/version":      "7.1.0",
				}, specCtx.Labels)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := podSpecContext(tt.params, "operator-image", newEnvVarsFn, newESConfigFn, newInitContainersFn)
			require.NoError(t, err)
			tt.assertions(t, spec)
		})
	}
}
