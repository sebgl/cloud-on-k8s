// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package user

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	esv1 "github.com/elastic/cloud-on-k8s/pkg/apis/elasticsearch/v1"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/user/filerealm"
	"github.com/elastic/cloud-on-k8s/pkg/utils/k8s"
)

func Test_ReconcileRolesFileRealmSecret(t *testing.T) {
	c := k8s.WrappedFakeClient()
	es := esv1.Elasticsearch{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "es"}}
	roles := RolesFileContent{"click_admins": []byte(`run_as: [ 'clicks_watcher_1' ]
  cluster: [ 'monitor' ]
  indices:
  - names: [ 'events-*' ]
    privileges: [ 'read' ]
    field_security:
      grant: ['category', '@timestamp', 'message' ]
    query: '{"match": {"category": "click"}}'`)}
	realm := filerealm.New().
		WithUser("user1", []byte("hash1")).
		WithUser("user2", []byte("hash2")).
		WithRole("role1", []string{"user1"}).
		WithRole("role2", []string{"user2"})

	err := ReconcileRolesFileRealmSecret(c, es, roles, realm)
	require.NoError(t, err)
	// retrieve reconciled secret
	var secret corev1.Secret
	err = c.Get(types.NamespacedName{Namespace: es.Namespace, Name: esv1.RolesAndFileRealmSecret(es.Name)}, &secret)
	require.NoError(t, err)
	require.Len(t, secret.Data, 3)
	require.Contains(t, string(secret.Data[ElasticRolesFile]), "click_admins")
	require.Contains(t, string(secret.Data[filerealm.UsersRolesFile]), "role1:user1")
	require.Contains(t, string(secret.Data[filerealm.UsersFile]), "user1:hash1")
}
