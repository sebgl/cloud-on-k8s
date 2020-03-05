// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package user

import (
	"fmt"
	"reflect"
	"strings"

	esv1 "github.com/elastic/cloud-on-k8s/pkg/apis/elasticsearch/v1"
	"github.com/elastic/cloud-on-k8s/pkg/controller/common/reconciler"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/label"
	"github.com/elastic/cloud-on-k8s/pkg/utils/k8s"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
)

func ReconcileRolesFileRealmSecret(c k8s.Client, es esv1.Elasticsearch, roles RolesFileContent, fileRealm FileRealm) error {
	rolesBytes, err := roles.FileBytes()
	if err != nil {
		return err
	}
	expected := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: es.Namespace,
			Name:      XPackFileRealmSecretName(es.Name),
			Labels:    label.NewLabels(k8s.ExtractNamespacedName(&es)),
		},
		Data: map[string][]byte{
			ElasticUsersFile:      fileRealm.Users.FileBytes(),
			ElasticUsersRolesFile: fileRealm.UsersRoles.FileBytes(),
			ElasticRolesFile:      rolesBytes,
		},
	}
	var reconciled corev1.Secret
	return reconciler.ReconcileResource(reconciler.Params{
		Client:     c,
		Scheme:     scheme.Scheme,
		Owner:      &es,
		Expected:   &expected,
		Reconciled: &reconciled,
		NeedsUpdate: func() bool {
			return !reflect.DeepEqual(expected.Data, reconciled.Data)
		},
		UpdateReconciled: func() {
			reconciled.Data = expected.Data
		},
	})
}

// RetrieveUserProvidedFileRealm builds a FileRealm from aggregated user-provided secrets specified in the es spec.
func RetrieveUserProvidedFileRealm(c k8s.Client, es esv1.Elasticsearch) (FileRealm, error) {
	fileRealm := FileRealm{
		Users:      make(usersPasswordHashes),
		UsersRoles: make(roleUsersMapping),
	}
	for _, fileRealmSource := range es.Spec.Auth.FileRealm {
		if fileRealmSource.SecretName == "" {
			continue
		}
		var secret corev1.Secret
		if err := c.Get(types.NamespacedName{Namespace: es.Namespace, Name: fileRealmSource.SecretName}, &secret); err != nil {
			return FileRealm{}, err
		}
		users, err := parseFileRealmUsers(k8s.GetSecretEntry(secret, ElasticUsersFile))
		if err != nil {
			return FileRealm{}, errors.Wrap(err, fmt.Sprintf("fail to parse users from secret %s", secret.Name))
		}
		usersRoles, err := parseFileRealmUsersRoles(k8s.GetSecretEntry(secret, ElasticUsersRolesFile))
		if err != nil {
			return FileRealm{}, errors.Wrap(err, fmt.Sprintf("fail to parse users from secret %s", secret.Name))
		}
		fileRealm.MergeWith(FileRealm{Users: users, UsersRoles: usersRoles})
	}
	return fileRealm, nil
}

func parseFileRealmUsers(data []byte) (usersPasswordHashes, error) {
	usersHashes := make(usersPasswordHashes)
	rows := strings.Split(string(data), "\n")
	for _, row := range rows {
		userHash := strings.Split(row, ":")
		if len(userHash) != 2 {
			return nil, fmt.Errorf("invalid entry in users")
		}
		userName := userHash[0]
		passwordHash := userHash[1]
		usersHashes = usersHashes.With(userName, passwordHash)
	}
	return usersHashes, nil
}

func parseFileRealmUsersRoles(data []byte) (roleUsersMapping, error) {
	rolesMapping := make(roleUsersMapping)
	rows := strings.Split(string(data), "\n")
	for _, row := range rows {
		roleUsers := strings.Split(row, ":")
		if len(roleUsers) != 2 {
			return nil, fmt.Errorf("invalid entry in users_roles")
		}
		role := roleUsers[0]
		users := strings.Split(roleUsers[1], ",")
		rolesMapping = rolesMapping.With(role, users)
	}
	return rolesMapping, nil
}
