// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package user

import (
	"context"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/elastic/cloud-on-k8s/pkg/controller/common/reconciler"
	"github.com/elastic/cloud-on-k8s/pkg/controller/common/tracing"
	"github.com/elastic/cloud-on-k8s/pkg/controller/common/watches"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/client"
	esclient "github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/client"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/label"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/user/filerealm"
	"github.com/elastic/cloud-on-k8s/pkg/utils/maps"

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

	// build aggregate roles and file realms
	roles, err := aggregateRoles(c, es, watched)
	if err != nil {
		return client.UserAuth{}, err
	}
	fileRealm, controllerUser, err := aggregateFileRealm(c, es, watched)
	if err != nil {
		return client.UserAuth{}, err
	}

	// reconcile the aggregate secret
	if err := reconcileRolesFileRealmSecret(c, es, roles, fileRealm); err != nil {
		return client.UserAuth{}, err
	}

	// return the controller user for next reconciliation steps to interact with Elasticsearch
	return controllerUser, nil
}

// aggregateFileRealm aggregates the various file realms into a single one, and returns the controller user auth.
func aggregateFileRealm(
	c k8s.Client,
	es esv1.Elasticsearch,
	watched watches.DynamicWatches,
) (filerealm.Realm, esclient.UserAuth, error) {
	// retrieve existing file realm to reuse predefined users password hashes if possible
	existingFileRealm, err := filerealm.FromSecret(c, RolesFileRealmSecretKey(es))
	if err != nil && apierrors.IsNotFound(err) {
		// no secret yet, work with an empty file realm
		existingFileRealm = filerealm.New()
	} else if err != nil {
		return filerealm.Realm{}, esclient.UserAuth{}, err
	}

	// reconcile predefined users
	elasticUser, err := reconcileElasticUser(c, es, existingFileRealm)
	if err != nil {
		return filerealm.Realm{}, esclient.UserAuth{}, err
	}
	internalUsers, err := reconcileInternalUsers(c, es, existingFileRealm)
	if err != nil {
		return filerealm.Realm{}, esclient.UserAuth{}, err
	}
	// grab the controller user auth for later use
	controllerUserAuth, err := internalUsers.userAuth(ControllerUserName)
	if err != nil {
		return filerealm.Realm{}, esclient.UserAuth{}, err
	}

	// fetch associated users
	associatedUsers, err := retrieveAssociatedUsers(c, es)
	if err != nil {
		return filerealm.Realm{}, esclient.UserAuth{}, err
	}

	// watch & fetch user-provided file realm & roles
	userProvidedFileRealm, err := reconcileUserProvidedFileRealm(c, es, watched)
	if err != nil {
		return filerealm.Realm{}, esclient.UserAuth{}, err
	}

	// merge all file realm together, the last one having precedence
	fileRealm := filerealm.MergedFrom(
		internalUsers.fileRealm(),
		elasticUser.fileRealm(),
		associatedUsers.fileRealm(),
		userProvidedFileRealm,
	)

	return fileRealm, controllerUserAuth, nil
}

func aggregateRoles(c k8s.Client, es esv1.Elasticsearch, watched watches.DynamicWatches) (RolesFileContent, error) {
	userProvided, err := reconcileUserProvidedRoles(c, es, watched)
	if err != nil {
		return RolesFileContent{}, err
	}
	// merge all roles together, the last one having precedence
	return PredefinedRoles.MergeWith(userProvided), nil
}

// RolesFileRealmSecretKey returns a reference to the K8s secret holding the roles and file realm data.
func RolesFileRealmSecretKey(es esv1.Elasticsearch) types.NamespacedName {
	return types.NamespacedName{Namespace: es.Namespace, Name: esv1.RolesAndFileRealmSecret(es.Name)}
}

// reconcileRolesFileRealmSecret creates or updates the single secret holding the file realm and the file-based roles.
func reconcileRolesFileRealmSecret(c k8s.Client, es esv1.Elasticsearch, roles RolesFileContent, fileRealm filerealm.Realm) error {
	secretData := fileRealm.FileBytes()
	rolesBytes, err := roles.FileBytes()
	if err != nil {
		return err
	}
	secretData[RolesFile] = rolesBytes

	expected := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: RolesFileRealmSecretKey(es).Namespace,
			Name:      RolesFileRealmSecretKey(es).Name,
			Labels:    label.NewLabels(k8s.ExtractNamespacedName(&es)),
		},
		Data: secretData,
	}
	// TODO: factorize with https://github.com/elastic/cloud-on-k8s/issues/2626
	var reconciled corev1.Secret
	return reconciler.ReconcileResource(reconciler.Params{
		Client:     c,
		Scheme:     scheme.Scheme,
		Owner:      &es,
		Expected:   &expected,
		Reconciled: &reconciled,
		NeedsUpdate: func() bool {
			// update if secret content is different
			return !reflect.DeepEqual(expected.Data, reconciled.Data) ||
				// or expected labels are not there
				!maps.IsSubset(expected.Labels, reconciled.Labels)
		},
		UpdateReconciled: func() {
			reconciled.Data = expected.Data
			maps.Merge(reconciled.Labels, expected.Labels)
			reconciled.Annotations = maps.Merge(reconciled.Annotations, expected.Annotations)
		},
	})
}
