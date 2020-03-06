// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package user

import (
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	esv1 "github.com/elastic/cloud-on-k8s/pkg/apis/elasticsearch/v1"
	commonuser "github.com/elastic/cloud-on-k8s/pkg/controller/common/user"
	"github.com/elastic/cloud-on-k8s/pkg/utils/k8s"
)

// retrieveAssociatedUsers fetches users resulting from an association (eg. Kibana or APMServer users).
func retrieveAssociatedUsers(c k8s.Client, es esv1.Elasticsearch) (users, error) {
	// list all associated users secret
	var associatedUserSecrets corev1.SecretList
	matchLabels := commonuser.NewLabelSelectorForElasticsearch(es)
	if err := c.List(&associatedUserSecrets, client.InNamespace(es.Namespace), matchLabels); err != nil {
		return nil, err
	}

	// parse secrets content into Users
	users := make(users, 0, len(associatedUserSecrets.Items))
	for _, secret := range associatedUserSecrets.Items {
		u, err := commonuser.NewExternalUserFromSecret(secret)
		if err != nil {
			return nil, err
		}
		passwordHash, err := u.PasswordHash()
		if err != nil {
			return nil, err
		}
		users = append(users, user{Name: u.Id(), PasswordHash: passwordHash, Roles: u.Roles()})
	}

	return users, nil
}
