// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package user

import (
	"bytes"
	"fmt"
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

func RolesFileRealmSecretKey(es esv1.Elasticsearch) types.NamespacedName {
	return types.NamespacedName{Namespace: es.Namespace, Name: esv1.RolesAndFileRealmSecret(es.Name)}
}

func reconcileRolesFileRealmSecret(c k8s.Client, es esv1.Elasticsearch, roles rolesFileContent, fileRealm fileRealm) error {
	nsn := RolesFileRealmSecretKey(es)
	rolesBytes, err := roles.FileBytes()
	if err != nil {
		return err
	}
	expected := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nsn.Name,
			Namespace: nsn.Namespace,
			Labels:    label.NewLabels(k8s.ExtractNamespacedName(&es)),
		},
		Data: map[string][]byte{
			ElasticUsersFile:      fileRealm.Users.FileBytes(),
			ElasticUsersRolesFile: fileRealm.UsersRoles.FileBytes(),
			ElasticRolesFile:      rolesBytes,
		},
	}
	_, err = reconciler.ReconcileSecret(c, &es, scheme.Scheme, expected)
	return err
}

func fileRealmFromSecret(c k8s.Client, secretKey types.NamespacedName) (fileRealm, error) {
	var secret corev1.Secret
	if err := c.Get(secretKey, &secret); err != nil {
		return fileRealm{}, err
	}
	users, err := parseFileRealmUsers(k8s.GetSecretEntry(secret, ElasticUsersFile))
	if err != nil {
		return fileRealm{}, errors.Wrap(err, fmt.Sprintf("fail to parse users from secret %s", secret.Name))
	}
	usersRoles, err := parseFileRealmUsersRoles(k8s.GetSecretEntry(secret, ElasticUsersRolesFile))
	if err != nil {
		return fileRealm{}, errors.Wrap(err, fmt.Sprintf("fail to parse users roles from secret %s", secret.Name))
	}
	return fileRealm{Users: users, UsersRoles: usersRoles}, nil
}

func parseFileRealmUsers(data []byte) (usersPasswordHashes, error) {
	usersHashes := make(usersPasswordHashes)
	return usersHashes, forEachRow(data, func(row []byte) error {
		userHash := bytes.Split(row, []byte(":"))
		if len(userHash) != 2 {
			return fmt.Errorf("invalid entry in users")
		}
		usersHashes = usersHashes.MergeWith(usersPasswordHashes{
			string(userHash[0]): userHash[1], // user: password hash
		})
		return nil
	})
}

func parseFileRealmUsersRoles(data []byte) (roleUsersMapping, error) {
	rolesMapping := make(roleUsersMapping)
	return rolesMapping, forEachRow(data, func(row []byte) error {
		roleUsers := strings.Split(string(row), ":")
		if len(roleUsers) != 2 {
			return fmt.Errorf("invalid entry in users_roles")
		}
		rolesMapping = rolesMapping.MergeWith(roleUsersMapping{
			roleUsers[0]: strings.Split(roleUsers[1], ","), // user: []roles
		})
		return nil
	})
}

func forEachRow(data []byte, f func(row []byte) error) error {
	rows := bytes.Split(data, []byte("\n"))
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		if err := f(row); err != nil {
			return err
		}
	}
	return nil
}
