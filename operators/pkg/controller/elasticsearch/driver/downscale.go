// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package driver

import (
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"

	"github.com/elastic/cloud-on-k8s/operators/pkg/apis/elasticsearch/v1alpha1"
	"github.com/elastic/cloud-on-k8s/operators/pkg/controller/common/events"
	"github.com/elastic/cloud-on-k8s/operators/pkg/controller/common/reconciler"
	esclient "github.com/elastic/cloud-on-k8s/operators/pkg/controller/elasticsearch/client"
	"github.com/elastic/cloud-on-k8s/operators/pkg/controller/elasticsearch/label"
	"github.com/elastic/cloud-on-k8s/operators/pkg/controller/elasticsearch/migration"
	"github.com/elastic/cloud-on-k8s/operators/pkg/controller/elasticsearch/observer"
	"github.com/elastic/cloud-on-k8s/operators/pkg/controller/elasticsearch/reconcile"
	"github.com/elastic/cloud-on-k8s/operators/pkg/controller/elasticsearch/sset"
	"github.com/elastic/cloud-on-k8s/operators/pkg/controller/elasticsearch/version/zen1"
	"github.com/elastic/cloud-on-k8s/operators/pkg/controller/elasticsearch/version/zen2"
	"github.com/elastic/cloud-on-k8s/operators/pkg/utils/k8s"
)

// downscaleContext holds the context of this downscale, including clients and states,
// propagated from the main driver.
type downscaleContext struct {
	// clients
	k8sClient k8s.Client
	esClient  esclient.Client
	// driver states
	resourcesState reconcile.ResourcesState
	observedState  observer.State
	reconcileState *reconcile.State
	expectations   *reconciler.Expectations
	// ES cluster
	es v1alpha1.Elasticsearch
}

// HandleDownscale attempts to downscale actual StatefulSets towards expected ones.
func HandleDownscale(
	downscaleCtx downscaleContext,
	expectedStatefulSets sset.StatefulSetList,
	actualStatefulSets sset.StatefulSetList,
) *reconciler.Results {
	results := &reconciler.Results{}

	canProceed, err := noOnGoingDeletion(downscaleCtx, actualStatefulSets)
	if err != nil {
		return results.WithError(err)
	}
	if !canProceed {
		return results.WithResult(defaultRequeue)
	}

	// compute the list of StatefulSet downscales to perform
	downscales := calculateDownscales(expectedStatefulSets, actualStatefulSets)
	leavingNodes := leavingNodeNames(downscales)

	// migrate data away from nodes that should be removed
	if err := scheduleDataMigrations(downscaleCtx.esClient, leavingNodes); err != nil {
		return results.WithError(err)
	}

	for _, downscale := range downscales {
		// attempt the StatefulSet downscale (may or may not remove nodes)
		requeue, err := attemptDownscale(downscaleCtx, downscale, leavingNodes, actualStatefulSets)
		if err != nil {
			return results.WithError(err)
		}
		if requeue {
			// retry downscaling this statefulset later
			results.WithResult(defaultRequeue)
		}
	}

	return results
}

// noOnGoingDeletion returns true if some pods deletion or creation may still be in progress
func noOnGoingDeletion(downscaleCtx downscaleContext, actualStatefulSets sset.StatefulSetList) (bool, error) {
	// Pods we have may not match replicas specified in the StatefulSets spec.
	// This can happen if, for example, replicas were recently downscaled to remove a node,
	// but the node isn't completely terminated yet, and may still be part of the cluster.
	// Moving on with downscaling more nodes may lead to complications when dealing with
	// Elasticsearch shards allocation excludes (should not be cleared if the ghost node isn't removed yet)
	// or zen settings (must consider terminating masters that are still there).
	// Let's retry once expected pods are there.
	// PodReconciliationDone also matches any pod not created yet, for which we'll also requeue.
	return actualStatefulSets.PodReconciliationDone(downscaleCtx.k8sClient, downscaleCtx.es)
}

// ssetDownscale helps with the downscale of a single StatefulSet.
// A StatefulSet removal (going from 0 to 0 replicas) is also considered as a Downscale here.
type ssetDownscale struct {
	statefulSet     appsv1.StatefulSet
	initialReplicas int32
	targetReplicas  int32
}

// leavingNodeNames returns names of the nodes that are supposed to leave the Elasticsearch cluster
// for this StatefulSet. They are ordered by highest ordinal first;
func (d ssetDownscale) leavingNodeNames() []string {
	if d.targetReplicas >= d.initialReplicas {
		return nil
	}
	leavingNodes := make([]string, 0, d.initialReplicas-d.targetReplicas)
	for i := d.initialReplicas - 1; i >= d.targetReplicas; i-- {
		leavingNodes = append(leavingNodes, sset.PodName(d.statefulSet.Name, i))
	}
	return leavingNodes
}

// isRemoval returns true if this downscale is a StatefulSet removal.
func (d ssetDownscale) isRemoval() bool {
	// StatefulSet does not have any replica, and should not have one
	return d.initialReplicas == 0 && d.targetReplicas == 0
}

// isReplicaDecrease returns true if this downscale corresponds to decreasing replicas.
func (d ssetDownscale) isReplicaDecrease() bool {
	return d.targetReplicas < d.initialReplicas
}

// leavingNodeNames returns the names of all nodes that should leave the cluster (across StatefulSets).
func leavingNodeNames(downscales []ssetDownscale) []string {
	leavingNodes := []string{}
	for _, d := range downscales {
		leavingNodes = append(leavingNodes, d.leavingNodeNames()...)
	}
	return leavingNodes
}

// calculateDownscales compares expected and actual StatefulSets to return a list of ssetDownscale.
// We also include StatefulSets removal (0 replicas) in those downscales.
func calculateDownscales(expectedStatefulSets sset.StatefulSetList, actualStatefulSets sset.StatefulSetList) []ssetDownscale {
	downscales := []ssetDownscale{}
	for _, actualSset := range actualStatefulSets {
		actualReplicas := sset.Replicas(actualSset)
		expectedSset, shouldExist := expectedStatefulSets.GetByName(actualSset.Name)
		expectedReplicas := int32(0) // sset removal
		if shouldExist {             // sset downscale
			expectedReplicas = sset.Replicas(expectedSset)
		}
		if expectedReplicas == 0 || // removal
			expectedReplicas < actualReplicas { // downscale
			downscales = append(downscales, ssetDownscale{
				statefulSet:     actualSset,
				initialReplicas: actualReplicas,
				targetReplicas:  expectedReplicas,
			})
		}
	}
	return downscales
}

// scheduleDataMigrations requests Elasticsearch to migrate data away from leavingNodes.
// If leavingNodes is empty, it clears any existing settings.
func scheduleDataMigrations(esClient esclient.Client, leavingNodes []string) error {
	if len(leavingNodes) != 0 {
		log.V(1).Info("Migrating data away from nodes", "nodes", leavingNodes)
	}
	return migration.MigrateData(esClient, leavingNodes)
}

// attemptDownscale attempts to decrement the number of replicas of the given StatefulSet,
// or deletes the StatefulSet entirely if it should not contain any replica.
// Nodes whose data migration is not over will not be removed.
// A boolean is returned to indicate if a requeue should be scheduled if the entire downscale could not be performed.
func attemptDownscale(ctx downscaleContext, downscale ssetDownscale, allLeavingNodes []string, statefulSets sset.StatefulSetList) (bool, error) {
	switch {
	case downscale.isRemoval():
		ssetLogger(downscale.statefulSet).Info("Deleting statefulset")
		return false, ctx.k8sClient.Delete(&downscale.statefulSet)

	case downscale.isReplicaDecrease():
		// adjust the theoretical downscale to one we can safely perform
		performable := calculatePerformableDownscale(ctx, downscale, allLeavingNodes)
		if !performable.isReplicaDecrease() {
			// no downscale can be performed for now, let's requeue
			return true, nil
		}
		// do performable downscale, and requeue if needed
		shouldRequeue := performable.targetReplicas != downscale.targetReplicas
		return shouldRequeue, doDownscale(ctx, performable, statefulSets)

	default:
		// nothing to do
		return false, nil
	}
}

// calculatePerformableDownscale updates the given downscale target replicas to account for nodes
// which cannot be safely deleted yet.
// It returns the updated downscale and a boolean indicating whether a requeue should be done.
func calculatePerformableDownscale(
	ctx downscaleContext,
	downscale ssetDownscale,
	allLeavingNodes []string,
) ssetDownscale {
	// TODO: only one master node downscale at a time

	// create another downscale based on the provided one, for which we'll slowly decrease target replicas
	performableDownscale := ssetDownscale{
		statefulSet:     downscale.statefulSet,
		initialReplicas: downscale.initialReplicas,
		targetReplicas:  downscale.initialReplicas, // target set to initial
	}
	// iterate on all leaving nodes (ordered by highest ordinal first)
	for _, node := range downscale.leavingNodeNames() {
		if migration.IsMigratingData(ctx.observedState, node, allLeavingNodes) {
			ssetLogger(downscale.statefulSet).V(1).Info("Data migration not over yet, skipping node deletion", "node", node)
			ctx.reconcileState.UpdateElasticsearchMigrating(ctx.resourcesState, ctx.observedState)
			// no need to check other nodes since we remove them in order and this one isn't ready anyway
			return performableDownscale
		}
		// data migration over: allow pod to be removed
		performableDownscale.targetReplicas--
	}
	return performableDownscale
}

// doDownscale schedules nodes removal for the given downscale, and updates zen settings accordingly.
func doDownscale(downscaleCtx downscaleContext, downscale ssetDownscale, actualStatefulSets sset.StatefulSetList) error {
	ssetLogger(downscale.statefulSet).Info(
		"Scaling replicas down",
		"from", downscale.initialReplicas,
		"to", downscale.targetReplicas,
	)

	if err := updateZenSettingsForDownscale(downscaleCtx, downscale, actualStatefulSets); err != nil {
		return err
	}

	downscale.statefulSet.Spec.Replicas = &downscale.targetReplicas
	if err := downscaleCtx.k8sClient.Update(&downscale.statefulSet); err != nil {
		return err
	}

	// Expect the updated statefulset in the cache for next reconciliation.
	downscaleCtx.expectations.ExpectGeneration(downscale.statefulSet.ObjectMeta)

	return nil
}

// updateZenSettingsForDownscale makes sure zen1 and zen2 settings are updated to account for nodes
// that will soon be removed.
func updateZenSettingsForDownscale(ctx downscaleContext, downscale ssetDownscale, actualStatefulSets sset.StatefulSetList) error {
	if !label.IsMasterNodeSet(downscale.statefulSet) {
		// nothing to do
		return nil
	}

	// Maybe update zen1 minimum_master_nodes.
	if err := maybeUpdateZen1ForDownscale(ctx, actualStatefulSets); err != nil {
		return err
	}

	// Maybe update zen2 settings to exclude leaving master nodes from voting.
	if err := zen2.AddToVotingConfigExclusions(ctx.esClient, downscale.statefulSet, downscale.leavingNodeNames()); err != nil {
		return err
	}

	return nil
}

// maybeUpdateZen1ForDownscale updates zen1 minimum master nodes if we are downscaling from 2 to 1 master node.
func maybeUpdateZen1ForDownscale(ctx downscaleContext, actualStatefulSets sset.StatefulSetList) error {
	if !zen1.AtLeastOneNodeCompatibleWithZen1(actualStatefulSets) {
		return nil
	}
	// If we are moving from 2 to 1 master nodes, we need to update minimum_master_nodes before removing
	// the 2nd node, otherwise the cluster won't be able to form anymore.
	// This is inherently unsafe (can cause split brains), but there's no alternative.
	// For other situations (eg. 3 -> 2), it's fine to update minimum_master_nodes after the node is removed
	// (will be done at next reconciliation, before nodes removal).

	// retrieve the number of masters currently running ready
	actualPods, err := sset.GetActualPodsForCluster(ctx.k8sClient, ctx.es)
	if err != nil {
		return err
	}
	mastersReady := reconcile.AvailableElasticsearchNodes(label.FilterMasterNodePods(actualPods))
	if len(mastersReady) != 2 {
		// since we only remove one master at once, we can consider we are not in the 2->1 situation
		return nil
	}

	// there are 2 masters and we are about to downscale a StatefulSet with master nodes: 2->1 situation
	ctx.reconcileState.AddEvent(
		v1.EventTypeWarning, events.EventReasonUnhealthy,
		"Downscaling from 2 to 1 master nodes",
	)
	minimumMasterNodes := 1
	return zen1.UpdateMinimumMasterNodesTo(ctx.es, ctx.esClient, actualStatefulSets, ctx.reconcileState, minimumMasterNodes)
}
