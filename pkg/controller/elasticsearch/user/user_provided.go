// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package user

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	esv1 "github.com/elastic/cloud-on-k8s/pkg/apis/elasticsearch/v1"
	"github.com/elastic/cloud-on-k8s/pkg/controller/common/watches"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/user/filerealm"
	"github.com/elastic/cloud-on-k8s/pkg/utils/k8s"
)

// UserProvidedAuthWatchName returns the watch registered for user-provided auth secrets.
func UserProvidedAuthWatchName(es types.NamespacedName) string {
	return fmt.Sprintf("%s-%s-user-auth", es.Namespace, es.Name)
}

// ReconcileUserProvidedAuth returns an aggregated file realm and roles from the referenced sources in the es spec.
// It also ensures referenced secrets are watched for future reconciliations to be triggered on any change.
func ReconcileUserProvidedAuth(c k8s.Client, es esv1.Elasticsearch, watched watches.DynamicWatches) (filerealm.Realm, RolesFileContent, error) {
	// setup watches on user-provided auth secrets
	esKey := k8s.ExtractNamespacedName(&es)
	if err := watches.WatchUserProvidedSecrets(
		esKey,
		watched,
		UserProvidedAuthWatchName(esKey),
		es.Spec.Auth.SecretNames(),
	); err != nil {
		return filerealm.Realm{}, nil, err
	}
	// return user-provided file realm and roles
	realm, err := retrieveUserProvidedFileRealm(c, es)
	if err != nil {
		return filerealm.Realm{}, nil, err
	}
	roles, err := retrieveUserProvidedRoles(c, es)
	if err != nil {
		return filerealm.Realm{}, nil, err
	}
	return realm, roles, nil
}

// retrieveUserProvidedRoles returns roles parsed from user-provided secrets specified in the es spec.
func retrieveUserProvidedRoles(c k8s.Client, es esv1.Elasticsearch) (RolesFileContent, error) {
	roles := make(RolesFileContent)
	for _, roleSource := range es.Spec.Auth.Roles {
		if roleSource.SecretName == "" {
			continue
		}
		var secret corev1.Secret
		if err := c.Get(types.NamespacedName{Namespace: es.Namespace, Name: roleSource.SecretName}, &secret); err != nil {
			return nil, err
		}
		data := k8s.GetSecretEntry(secret, ElasticRolesFile)
		parsed, err := parseRolesFileContent(data)
		if err != nil {
			return nil, err
		}
		roles = roles.MergeWith(parsed)
	}
	return roles, nil
}

// retrieveUserProvidedFileRealm builds a Realm from aggregated user-provided secrets specified in the es spec.
func retrieveUserProvidedFileRealm(c k8s.Client, es esv1.Elasticsearch) (filerealm.Realm, error) {
	aggregated := filerealm.New()
	for _, fileRealmSource := range es.Spec.Auth.FileRealm {
		if fileRealmSource.SecretName == "" {
			continue
		}
		secretKey := types.NamespacedName{Namespace: es.Namespace, Name: fileRealmSource.SecretName}
		realm, err := filerealm.FromSecret(c, secretKey)
		if err != nil {
			return filerealm.Realm{}, err
		}
		aggregated = aggregated.MergeWith(realm)
	}
	return aggregated, nil
}
