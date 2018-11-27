package state

import (
	"github.com/elastic/stack-operators/stack-operator/pkg/apis/deployments/v1alpha1"
	"github.com/elastic/stack-operators/stack-operator/pkg/controller/stack/elasticsearch"
	"k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ReconcileState holds the accumulated state during the reconcile loop including the response and a pointer to a stack
// resource for status updates.
type ReconcileState struct {
	Stack   *v1alpha1.Stack
	Result  reconcile.Result
	Request reconcile.Request
}

// NewReconcileState creates a new reconcile state based on the given request and stack resource with the resource state
// reset to empty.
func NewReconcileState(request reconcile.Request, stack *v1alpha1.Stack) ReconcileState {
	// reset status to reconstruct it during the reconcile cycle
	stack.Status = v1alpha1.StackStatus{}
	return ReconcileState{Request: request, Stack: stack}
}

// UpdateKibanaState updates the Kibana section of the stack resource status based on the given deployment.
func (s ReconcileState) UpdateKibanaState(deployment v1.Deployment) {
	s.Stack.Status.Kibana.AvailableNodes = int(deployment.Status.AvailableReplicas) // TODO lossy type conversion
	s.Stack.Status.Kibana.Health = v1alpha1.KibanaRed
	for _, c := range deployment.Status.Conditions {
		if c.Type == v1.DeploymentAvailable && c.Status == corev1.ConditionTrue {
			s.Stack.Status.Kibana.Health = v1alpha1.KibanaGreen
		}
	}
}

// AvailableElasticsearchNodes filters a slice of pods for the ones that are ready.
func AvailableElasticsearchNodes(pods []corev1.Pod) []corev1.Pod {
	var nodesAvailable []corev1.Pod
	for _, pod := range pods {
		conditionsTrue := 0
		for _, cond := range pod.Status.Conditions {
			if cond.Status == corev1.ConditionTrue && (cond.Type == corev1.ContainersReady || cond.Type == corev1.PodReady) {
				conditionsTrue++
			}
		}
		if conditionsTrue == 2 {
			nodesAvailable = append(nodesAvailable, pod)
		}
	}
	return nodesAvailable
}

// UpdateElasticsearchState updates the Elasticsearch section of the state resource status based on the given pods.
func (s ReconcileState) UpdateElasticsearchState(
	state elasticsearch.ResourcesState,
) {
	s.Stack.Status.Elasticsearch.ClusterUUID = state.ClusterState.ClusterUUID
	s.Stack.Status.Elasticsearch.MasterNode = state.ClusterState.MasterNodeName()
	s.Stack.Status.Elasticsearch.AvailableNodes = len(AvailableElasticsearchNodes(state.CurrentPods))
	s.Stack.Status.Elasticsearch.Health = v1alpha1.ElasticsearchHealth("unknown")
	if s.Stack.Status.Elasticsearch.Phase == "" {
		s.Stack.Status.Elasticsearch.Phase = v1alpha1.ElasticsearchOperationalPhase
	}
	if state.ClusterHealth.Status != "" {
		s.Stack.Status.Elasticsearch.Health = v1alpha1.ElasticsearchHealth(state.ClusterHealth.Status)
	}
}

// UpdateElasticsearchPending marks Elasticsearch as being the pending phase in the resource status.
func (s ReconcileState) UpdateElasticsearchPending(result reconcile.Result, pods []corev1.Pod) {
	s.Stack.Status.Elasticsearch.AvailableNodes = len(AvailableElasticsearchNodes(pods))
	s.Stack.Status.Elasticsearch.Phase = v1alpha1.ElasticsearchPendingPhase
	s.Stack.Status.Elasticsearch.Health = v1alpha1.ElasticsearchRedHealth
	s.Result = result
}

// UpdateElasticsearchMigrating marks Elasticsearch as being in the data migration phase in the resource status.
func (s ReconcileState) UpdateElasticsearchMigrating(
	result reconcile.Result,
	state elasticsearch.ResourcesState,
) {
	s.Stack.Status.Elasticsearch.Phase = v1alpha1.ElasticsearchMigratingDataPhase
	s.Result = result
	s.UpdateElasticsearchState(state)
}
