// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package user

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/elastic/cloud-on-k8s/pkg/controller/common/tracing"
	"github.com/elastic/cloud-on-k8s/pkg/controller/common/watches"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/client"

	"go.elastic.co/apm"

	esv1 "github.com/elastic/cloud-on-k8s/pkg/apis/elasticsearch/v1"
	"github.com/elastic/cloud-on-k8s/pkg/utils/k8s"
)

// TODO better doc
func ReconcileUsersAndRoles(
	ctx context.Context,
	c k8s.Client,
	es esv1.Elasticsearch,
	watched watches.DynamicWatches,
) (client.UserAuth, error) {
	span, _ := apm.StartSpan(ctx, "reconcile_users", tracing.SpanTypeApp)
	defer span.End()

	// retrieve existing file realm to reuse predefined users password hashes if possible
	existingFileRealm, err := fileRealmFromSecret(c, RolesFileRealmSecretKey(es))
	if err != nil && apierrors.IsNotFound(err) {
		// no secret yet, work with an empty file realm
		existingFileRealm = newFileRealm()
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
	fileRealm := newFileRealm().MergeWith(
		internalUsers.FileRealm(),
		elasticUser.FileRealm(),
		associatedUsers.FileRealm(),
		userProvidedFileRealm, // has priority over the others
	)
	roles := PredefinedRoles.MergeWith(userProvidedRoles)

	// reconcile the aggregate secret
	if err := reconcileRolesFileRealmSecret(c, es, roles, fileRealm); err != nil {
		return client.UserAuth{}, err
	}

	// return the controller user for next reconciliation steps
	return internalUsers.UserAuth(ControllerUserName)
}
