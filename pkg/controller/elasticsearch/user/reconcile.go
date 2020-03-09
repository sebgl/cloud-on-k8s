// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package user

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/elastic/cloud-on-k8s/pkg/controller/common/reconciler"
	"github.com/elastic/cloud-on-k8s/pkg/controller/common/tracing"
	"github.com/elastic/cloud-on-k8s/pkg/controller/common/watches"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/client"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/label"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/user/filerealm"

	"go.elastic.co/apm"

	esv1 "github.com/elastic/cloud-on-k8s/pkg/apis/elasticsearch/v1"
	"github.com/elastic/cloud-on-k8s/pkg/utils/k8s"
)

// ReconcileUsersAndRoles fetches all users and roles and aggregates them into a single
// Kubernetes secret mounted in the Elasticsearch Pods.
// That secret contains the file realm files (`users` and `users_roles`) and the file roles (`roles.yml`).
// users are aggregated from various sources:
// - predefined users include the controller user, the probe user, and the public-facing elastic user
// - associated users come from resource associations (eg. Kibana or APMServer)
// - user-provided users from file realms referenced in the Elasticsearch spec
// Roles are aggregated from:
// - predefined roles (for the probe user)
// - user-provided roles referenced in the Elasticsearch spec
func ReconcileUsersAndRoles(
	ctx context.Context,
	c k8s.Client,
	es esv1.Elasticsearch,
	watched watches.DynamicWatches,
) (client.UserAuth, error) {
	span, _ := apm.StartSpan(ctx, "reconcile_users", tracing.SpanTypeApp)
	defer span.End()

	// retrieve existing file realm to reuse predefined users password hashes if possible
	existingFileRealm, err := filerealm.FromSecret(c, RolesFileRealmSecretKey(es))
	if err != nil && apierrors.IsNotFound(err) {
		// no secret yet, work with an empty file realm
		existingFileRealm = filerealm.New()
	} else if err != nil {
		return client.UserAuth{}, err
	}

	// reconcile predefined users
	internalUsers, err := reconcileInternalUsers(c, es, existingFileRealm)
	if err != nil {
		return client.UserAuth{}, err
	}
	elasticUser, err := reconcileElasticUser(c, es, existingFileRealm)
	if err != nil {
		return client.UserAuth{}, err
	}
	// fetch associated users
	associatedUsers, err := retrieveAssociatedUsers(c, es)
	if err != nil {
		return client.UserAuth{}, err
	}

	// watch & fetch user-provided file realm & roles
	userProvidedFileRealm, userProvidedRoles, err := ReconcileUserProvidedAuth(c, es, watched)
	if err != nil {
		return client.UserAuth{}, err
	}

	// build single merged file realm & roles
	fileRealm := filerealm.MergedFrom(
		internalUsers.fileRealm(),
		elasticUser.fileRealm(),
		associatedUsers.fileRealm(),
		userProvidedFileRealm, // has priority over the others
	)
	roles := PredefinedRoles.MergeWith(userProvidedRoles)

	// reconcile the aggregate secret
	if err := ReconcileRolesFileRealmSecret(c, es, roles, fileRealm); err != nil {
		return client.UserAuth{}, err
	}

	// return the controller user for next reconciliation steps to interact with Elasticsearch
	return internalUsers.userAuth(ControllerUserName)
}

// RolesFileRealmSecretKey returns a reference to the K8s secret holding the roles and file realm data.
func RolesFileRealmSecretKey(es esv1.Elasticsearch) types.NamespacedName {
	return types.NamespacedName{Namespace: es.Namespace, Name: esv1.RolesAndFileRealmSecret(es.Name)}
}

// ReconcileRolesFileRealmSecret creates or updates the single secret holding the file realm and the file-based roles.
func ReconcileRolesFileRealmSecret(c k8s.Client, es esv1.Elasticsearch, roles RolesFileContent, fileRealm filerealm.Realm) error {
	secretData := fileRealm.FileBytes()
	rolesBytes, err := roles.FileBytes()
	if err != nil {
		return err
	}
	secretData[ElasticRolesFile] = rolesBytes

	expected := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: RolesFileRealmSecretKey(es).Namespace,
			Name:      RolesFileRealmSecretKey(es).Name,
			Labels:    label.NewLabels(k8s.ExtractNamespacedName(&es)),
		},
		Data: secretData,
	}
	_, err = reconciler.ReconcileSecret(c, &es, scheme.Scheme, expected)
	return err
}
