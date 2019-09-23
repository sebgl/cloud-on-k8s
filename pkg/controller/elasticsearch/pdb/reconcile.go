// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package pdb

import (
	"reflect"

	"k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1alpha1 "github.com/elastic/cloud-on-k8s/pkg/apis/common/v1alpha1"
	"github.com/elastic/cloud-on-k8s/pkg/apis/elasticsearch/v1alpha1"
	"github.com/elastic/cloud-on-k8s/pkg/controller/common/defaults"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/label"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/name"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/sset"
	"github.com/elastic/cloud-on-k8s/pkg/utils/k8s"
)

// Reconcile ensures that a PodDisruptionBudget exists for this cluster, inheriting the spec content.
// The default PDB we setup dynamically adapts MinAvailable to the number of nodes in the cluster.
// If the spec has disabled the default PDB, it will ensure none exist.
func Reconcile(k8sClient k8s.Client, scheme *runtime.Scheme, es v1alpha1.Elasticsearch, statefulSets sset.StatefulSetList) error {
	expected, err := expectedPDB(es, statefulSets, scheme)
	if err != nil {
		return err
	}
	if expected == nil {
		return deleteDefaultPDB(k8sClient, es)
	}

	// reconcile actual vs. expected
	var actual v1beta1.PodDisruptionBudget
	err = k8sClient.Get(k8s.ExtractNamespacedName(expected), &actual)
	if err != nil && apierrors.IsNotFound(err) {
		return k8sClient.Create(expected)
	}

	// update if we're missing a label
	needsLabelUpdate := false
	for k, v := range expected.Labels {
		if actualValue, ok := actual.Labels[k]; !ok || actualValue != v {
			needsLabelUpdate = true
		}
	}

	if needsLabelUpdate || !reflect.DeepEqual(expected.Spec, actual.Spec) {
		// PDB Spec cannot be updated, we'll have to delete then recreate.
		// Which means there is a time window in between where we don't have a PDB anymore.
		// TODO: this is not true anymore starting k8s 1.15+ and this PR https://github.com/kubernetes/kubernetes/pull/69867
		if err := deleteDefaultPDB(k8sClient, es); err != nil {
			return err
		}
		return k8sClient.Create(expected)
	}

	return nil
}

// deleteDefaultPDB deletes the default pdb if it exists.
func deleteDefaultPDB(k8sClient k8s.Client, es v1alpha1.Elasticsearch) error {
	// we do this by getting first because that is a local cache read,
	// versus a Delete call, which would hit the API.
	pdb := v1beta1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: es.Namespace,
			Name:      name.DefaultPodDisruptionBudget(es.Name),
		},
	}
	if err := k8sClient.Get(k8s.ExtractNamespacedName(&pdb), &pdb); err != nil && !errors.IsNotFound(err) {
		return err
	} else if errors.IsNotFound(err) {
		// already deleted, which is fine
		return nil
	}
	if err := k8sClient.Delete(&pdb); err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

// expectedPDB returns a PDB according to the given ES spec.
// It may return nil if the PDB has been explicitly disabled in the ES spec.
func expectedPDB(es v1alpha1.Elasticsearch, statefulSets sset.StatefulSetList, scheme *runtime.Scheme) (*v1beta1.PodDisruptionBudget, error) {
	template := es.Spec.PodDisruptionBudget.DeepCopy()
	if template.IsDisabled() {
		return nil, nil
	}
	if template == nil {
		template = &commonv1alpha1.PodDisruptionBudgetTemplate{}
	}

	expected := v1beta1.PodDisruptionBudget{
		ObjectMeta: template.ObjectMeta,
	}

	// inherit user-provided ObjectMeta, but set our own name & namespace
	expected.Name = name.DefaultPodDisruptionBudget(es.Name)
	expected.Namespace = es.Namespace
	// and append our labels
	expected.Labels = defaults.SetDefaultLabels(expected.Labels, label.NewLabels(k8s.ExtractNamespacedName(&es)))
	// set owner reference for deletion upon ES resource deletion
	if err := controllerutil.SetControllerReference(&es, &expected, scheme); err != nil {
		return nil, err
	}

	if template.Spec.Selector != nil || template.Spec.MaxUnavailable != nil || template.Spec.MinAvailable != nil {
		// use the user-defined spec
		expected.Spec = template.Spec
	} else {
		// set our default spec
		expected.Spec = buildPDBSpec(es, statefulSets)
	}

	return &expected, nil
}

// buildPDBSpec returns a PDBSpec computed from the current StatefulSets,
// considering the cluster health and topology.
func buildPDBSpec(es v1alpha1.Elasticsearch, statefulSets sset.StatefulSetList) v1beta1.PodDisruptionBudgetSpec {
	// compute MinAvailable based on the maximum number of Pods we're supposed to have
	nodeCount := statefulSets.ExpectedPodCount()
	// maybe allow some Pods to be disrupted
	minAvailable := nodeCount - allowedDisruptions(es, statefulSets)

	minAvailableIntStr := intstr.IntOrString{Type: intstr.Int, IntVal: minAvailable}

	return v1beta1.PodDisruptionBudgetSpec{
		// match all pods for this cluster
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				label.ClusterNameLabelName: es.Name,
			},
		},
		MinAvailable: &minAvailableIntStr,
		// MaxUnavailable can only be used if the selector matches a builtin controller selector
		// (eg. Deployments, StatefulSets, etc.). We cannot use it with our own cluster-name selector.
		MaxUnavailable: nil,
	}
}

// allowedDisruptions returns the number of Pods that we allow to be disrupted while keeping the cluster healthy.
func allowedDisruptions(es v1alpha1.Elasticsearch, actualSsets sset.StatefulSetList) int32 {
	if es.Status.Health != v1alpha1.ElasticsearchGreenHealth {
		// A non-green cluster may become red if we disrupt one node, don't allow it.
		// The health information we're using here may be out-of-date, that's best effort.
		return 0
	}
	if actualSsets.ExpectedMasterNodesCount() == 1 {
		// There's a risk the single master of the cluster gets removed, don't allow it.
		return 0
	}
	if actualSsets.ExpectedDataNodesCount() == 1 {
		// There's a risk the single data node of the cluster gets removed, don't allow it.
		return 0
	}
	// Allow one pod (only) to be disrupted on a healthy cluster.
	// We could technically allow more, but the cluster health freshness would become a bigger problem.
	return 1
}
