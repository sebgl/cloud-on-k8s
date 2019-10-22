// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package sset

import (
	"reflect"
	"testing"

	"github.com/elastic/cloud-on-k8s/pkg/controller/common/version"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/label"
	"github.com/elastic/cloud-on-k8s/pkg/utils/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var ssetv7 = appsv1.StatefulSet{
	Spec: appsv1.StatefulSetSpec{
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					label.VersionLabelName: "7.1.0",
				},
			},
		},
	},
}

func TestESVersionMatch(t *testing.T) {
	require.Equal(t, true,
		ESVersionMatch(ssetv7, func(v version.Version) bool {
			return v.Major == 7
		}),
	)
	require.Equal(t, false,
		ESVersionMatch(ssetv7, func(v version.Version) bool {
			return v.Major == 6
		}),
	)
}

func TestAtLeastOneESVersionMatch(t *testing.T) {
	ssetv6 := *ssetv7.DeepCopy()
	ssetv6.Spec.Template.Labels[label.VersionLabelName] = "6.8.0"

	require.Equal(t, true,
		AtLeastOneESVersionMatch(StatefulSetList{ssetv6, ssetv7}, func(v version.Version) bool {
			return v.Major == 7
		}),
	)
	require.Equal(t, false,
		AtLeastOneESVersionMatch(StatefulSetList{ssetv6, ssetv6}, func(v version.Version) bool {
			return v.Major == 7
		}),
	)
}

func TestStatefulSetList_GetExistingPods(t *testing.T) {
	// 2 pods that belong to the sset
	pod1 := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod1",
			Labels: map[string]string{
				label.StatefulSetNameLabelName: ssetv7.Name,
			},
		},
	}
	pod2 := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod2",
			Labels: map[string]string{
				label.StatefulSetNameLabelName: ssetv7.Name,
			},
		},
	}
	// pod not belonging to the sset
	podNotInSset := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-not-in-sset",
			Labels: map[string]string{
				label.StatefulSetNameLabelName: "different-sset",
			},
		},
	}
	client := k8s.WrappedFakeClient(&pod1, &pod2, &podNotInSset)
	pods, err := StatefulSetList{ssetv7}.GetActualPods(client)
	require.NoError(t, err)
	require.Equal(t, []corev1.Pod{pod1, pod2}, pods)
	require.NotContains(t, pods, podNotInSset)
}

func TestStatefulSetList_PodReconciliationDone(t *testing.T) {
	// more detailed cases covered in PodReconciliationDoneForSset(), called by the function we test here
	tests := []struct {
		name string
		l    StatefulSetList
		c    k8s.Client
		want bool
	}{
		{
			name: "no pods, no sset",
			l:    nil,
			c:    k8s.WrappedFakeClient(),
			want: true,
		},
		{
			name: "some pods, no sset",
			l:    nil,
			c: k8s.WrappedFakeClient(
				TestPod{Namespace: "ns", Name: "sset-0", StatefulSetName: "sset", Revision: "current-rev"}.BuildPtr(),
			),
			want: true,
		},
		{
			name: "some statefulSets, no pod",
			l:    StatefulSetList{TestSset{Name: "sset1", Replicas: 3}.Build()},
			c:    k8s.WrappedFakeClient(TestSset{Name: "sset1", Replicas: 3}.BuildPtr()),
			want: false,
		},
		{
			name: "sset has its pods",
			l: StatefulSetList{
				TestSset{Name: "sset1", Namespace: "ns", Replicas: 2, Status: appsv1.StatefulSetStatus{CurrentRevision: "current-rev"}}.Build(),
			},
			c: k8s.WrappedFakeClient(
				TestPod{Namespace: "ns", Name: "sset1-0", StatefulSetName: "sset1", Revision: "current-rev"}.BuildPtr(),
				TestPod{Namespace: "ns", Name: "sset1-1", StatefulSetName: "sset1", Revision: "current-rev"}.BuildPtr(),
				TestPod{Namespace: "ns", Name: "sset2-0", StatefulSetName: "sset2", Revision: "current-rev"}.BuildPtr(),
				TestPod{Namespace: "ns0", Name: "sset1-0", StatefulSetName: "sset1", Revision: "current-rev"}.BuildPtr(),
			),
			want: true,
		},
		{
			name: "sset is missing a pod",
			l: StatefulSetList{
				TestSset{Name: "sset1", Replicas: 2, Status: appsv1.StatefulSetStatus{CurrentRevision: "current-rev"}}.Build(),
			},
			c: k8s.WrappedFakeClient(
				TestPod{Namespace: "ns", Name: "sset1-0", StatefulSetName: "sset2", Revision: "current-rev"}.BuildPtr(),
			),
			want: false,
		},
		// TODO: test more than one StatefulSet once https://github.com/kubernetes-sigs/controller-runtime/pull/311 is available
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.l.PodReconciliationDone(tt.c)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStatefulSetList_GetByName(t *testing.T) {
	sset := func(name string) appsv1.StatefulSet {
		return appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: name}}
	}
	tests := []struct {
		name       string
		l          StatefulSetList
		ssetName   string
		wantResult appsv1.StatefulSet
		wantFound  bool
	}{
		{
			name:      "statefulset not found",
			l:         StatefulSetList{sset("a"), sset("b")},
			ssetName:  "c",
			wantFound: false,
		},
		{
			name:       "statefulset found",
			l:          StatefulSetList{sset("a"), sset("b")},
			ssetName:   "b",
			wantFound:  true,
			wantResult: sset("b"),
		},
		{
			name:      "empty list",
			l:         StatefulSetList{},
			ssetName:  "b",
			wantFound: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, found := tt.l.GetByName(tt.ssetName)
			if !reflect.DeepEqual(result, tt.wantResult) {
				t.Errorf("GetByName() got = %v, want %v", result, tt.wantResult)
			}
			if found != tt.wantFound {
				t.Errorf("GetByName() got1 = %v, want %v", found, tt.wantFound)
			}
		})
	}
}

func TestStatefulSetList_ToUpdate(t *testing.T) {
	toUpdate1 := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "toUpdate1"},
		Status:     appsv1.StatefulSetStatus{UpdatedReplicas: 1, Replicas: 2},
	}
	toUpdate2 := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "toUpdate2"},
		Status:     appsv1.StatefulSetStatus{UpdatedReplicas: 1, Replicas: 2},
	}
	updateMatchCurrent := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "updateMatchCurrent"},
		Status:     appsv1.StatefulSetStatus{UpdatedReplicas: 1, Replicas: 1},
	}
	tests := []struct {
		name string
		l    StatefulSetList
		want StatefulSetList
	}{
		{
			name: "empty list",
			l:    StatefulSetList{},
			want: StatefulSetList{},
		},
		{
			name: "2/3 StatefulSets to update",
			l:    StatefulSetList{toUpdate1, updateMatchCurrent, toUpdate2},
			want: StatefulSetList{toUpdate1, toUpdate2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.l.ToUpdate(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ToUpdate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStatefulSetList_WithStatefulSet(t *testing.T) {
	tests := []struct {
		name        string
		l           StatefulSetList
		statefulSet appsv1.StatefulSet
		want        StatefulSetList
	}{
		{
			name:        "add a new StatefulSet",
			l:           StatefulSetList{TestSset{Namespace: "ns", Name: "sset1"}.Build()},
			statefulSet: TestSset{Namespace: "ns", Name: "sset2"}.Build(),
			want:        StatefulSetList{TestSset{Namespace: "ns", Name: "sset1"}.Build(), TestSset{Namespace: "ns", Name: "sset2"}.Build()},
		},
		{
			name:        "replace an existing StatefulSet",
			l:           StatefulSetList{TestSset{Namespace: "ns", Name: "sset1", Master: true}.Build()},
			statefulSet: TestSset{Namespace: "ns", Name: "sset1", Master: false}.Build(),
			want:        StatefulSetList{TestSset{Namespace: "ns", Name: "sset1", Master: false}.Build()},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.l.WithStatefulSet(tt.statefulSet); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WithStatefulSet() = %v, want %v", got, tt.want)
			}
		})
	}
}
