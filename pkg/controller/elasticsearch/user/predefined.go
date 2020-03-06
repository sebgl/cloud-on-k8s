// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package user

import (
	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"

	esv1 "github.com/elastic/cloud-on-k8s/pkg/apis/elasticsearch/v1"
	"github.com/elastic/cloud-on-k8s/pkg/controller/common/reconciler"
	commonuser "github.com/elastic/cloud-on-k8s/pkg/controller/common/user"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/label"
	"github.com/elastic/cloud-on-k8s/pkg/utils/k8s"
)

const (
	// ElasticUserName is a public-facing user.
	ElasticUserName = "elastic"

	// ControllerUserName a user to be used from this controller when interacting with ES.
	ControllerUserName = "elastic-internal"
	// ProbeUserName is a user to be used from the liveness/readiness probes when interacting with ES.
	ProbeUserName = "elastic-internal-probe"
)

func reconcileElasticUser(c k8s.Client, es esv1.Elasticsearch, existingFileRealm fileRealm) (users, error) {
	return reconcilePredefinedUsers(
		c,
		es,
		existingFileRealm,
		users{
			{Name: ElasticUserName, Roles: []string{SuperUserBuiltinRole}},
		},
		esv1.ElasticUserSecret(es.Name),
	)
}

func reconcileInternalUsers(c k8s.Client, es esv1.Elasticsearch, existingFileRealm fileRealm) (users, error) {
	return reconcilePredefinedUsers(
		c,
		es,
		existingFileRealm,
		users{
			{Name: ControllerUserName, Roles: []string{SuperUserBuiltinRole}},
			{Name: ProbeUserName, Roles: []string{ProbeUserRole}},
		},
		esv1.InternalUsersSecret(es.Name))
}

func reconcilePredefinedUsers(
	c k8s.Client,
	es esv1.Elasticsearch,
	existingFileRealm fileRealm,
	users users,
	secretName string,
) (users, error) {
	secretNsn := types.NamespacedName{Namespace: es.Namespace, Name: secretName}

	// build users, reusing existing passwords and bcrypt hashes if possible
	var err error
	users, err = reuseOrGeneratePassword(c, users, secretNsn)
	if err != nil {
		return nil, err
	}
	users, err = reuseOrGenerateHash(users, existingFileRealm)
	if err != nil {
		return nil, err
	}

	// reconcile secret
	secretData := make(map[string][]byte, len(users))
	for _, u := range users {
		secretData[u.Name] = u.Password
	}
	_, err = reconciler.ReconcileSecret(c, &es, scheme.Scheme, corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: secretNsn.Namespace,
			Name:      secretNsn.Name,
			Labels:    label.NewLabels(k8s.ExtractNamespacedName(&es)),
		},
		Data: secretData,
	})
	return users, err
}

// reuseOrGeneratePassword updates the users with existing passwords reused from the existing K8s secret,
// or generates new passwords.
func reuseOrGeneratePassword(c k8s.Client, users users, secretRef types.NamespacedName) (users, error) {
	var secret corev1.Secret
	err := c.Get(secretRef, &secret)
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}
	if apierrors.IsNotFound(err) || secret.Data == nil {
		// no password to reuse
		return users, nil
	}
	for i, u := range users {
		if password, exists := secret.Data[u.Name]; exists {
			users[i].Password = password
		} else {
			users[i].Password = commonuser.RandomPasswordBytes()
		}
	}
	return users, nil
}

// reuseOrGenerateHash updates the users with existing hashes or generates new ones.
func reuseOrGenerateHash(users users, fileRealm fileRealm) (users, error) {
	for i, u := range users {
		existingHash := fileRealm.PasswordHashForUser(u.Name)
		if bcrypt.CompareHashAndPassword(existingHash, u.Password) == nil {
			users[i].PasswordHash = existingHash
		} else {
			hash, err := bcrypt.GenerateFromPassword(u.Password, bcrypt.DefaultCost)
			if err != nil {
				return nil, err
			}
			users[i].PasswordHash = hash
		}
	}
	return users, nil
}
