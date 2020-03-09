// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package user

//
//const ()
//
//// NewToRequestsFuncFromClusterNameLabel creates a watch handler function that creates reconcile requests based on the
//// the cluster name label if the resource is of type "user".
//func NewToRequestsFuncFromClusterNameLabel() handler.ToRequestsFunc {
//	return handler.ToRequestsFunc(func(obj handler.MapObject) []reconcile.Request {
//		labels := obj.Meta.GetLabels()
//		if labelType, ok := labels[common.TypeLabelName]; !ok || labelType != UserType {
//			return []reconcile.Request{}
//		}
//
//		if clusterName, ok := labels[label.ClusterNameLabelName]; ok {
//			// we don't need to special case the handling of this label to support in-place changes to its value
//			// as controller-runtime will ask this func to map both the old and the new resources on updates.
//			return []reconcile.Request{
//				{NamespacedName: types.NamespacedName{Namespace: obj.Meta.GetNamespace(), Name: clusterName}},
//			}
//		}
//		return nil
//	})
//}
