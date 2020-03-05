// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package user

import (
	"fmt"

	esv1 "github.com/elastic/cloud-on-k8s/pkg/apis/elasticsearch/v1"
	esclient "github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/client"
	"github.com/elastic/cloud-on-k8s/pkg/utils/k8s"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// ElasticRolesFile is the name of the roles file in the ES config dir.
	ElasticRolesFile = "roles.yml"
)

var (
	// PredefinedRoles to create for "internal" needs.
	PredefinedRoles = RolesFileContent{
		ProbeUserRole: esclient.Role{Cluster: []string{"monitor"}},
	}
)

// RolesFileContent is a map {role name -> yaml role spec}.
type RolesFileContent map[string]interface{}

func (r RolesFileContent) MergeWith(other RolesFileContent) RolesFileContent {
	for roleName, roleSpec := range other {
		r[roleName] = roleSpec
	}
	return r
}

func (r RolesFileContent) FileBytes() ([]byte, error) {
	fmt.Println(r)
	return yaml.Marshal(&r)
}

func RetrieveUserProvidedRoles(c k8s.Client, es esv1.Elasticsearch) (RolesFileContent, error) {
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
		var parsed RolesFileContent
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			return nil, err
		}
		roles = roles.MergeWith(parsed)
	}
	return roles, nil
}
