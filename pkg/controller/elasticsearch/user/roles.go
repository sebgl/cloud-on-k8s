// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package user

import (
	esclient "github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/client"

	"gopkg.in/yaml.v2"
)

const (
	// ElasticRolesFile is the name of the roles file in the ES config dir.
	ElasticRolesFile = "roles.yml"

	// SuperUserBuiltinRole is the name of the built-in superuser role.
	SuperUserBuiltinRole = "superuser"
	// ProbeUserRole is the name of the role used by the internal probe user.
	ProbeUserRole = "elastic_internal_probe_user"
)

var (
	// PredefinedRoles to create for internal needs.
	PredefinedRoles = rolesFileContent{
		ProbeUserRole: esclient.Role{Cluster: []string{"monitor"}},
	}
)

// rolesFileContent is a map {role name -> yaml role spec}.
// We care about the role names here, but consider the roles spec as a bag of data we don't need to access.
// This can be marshalled/unmarshalled to/from the yaml file representation directly.
type rolesFileContent map[string]interface{}

// MergeWith merges multiple rolesFileContent, giving priority to other.
func (r rolesFileContent) MergeWith(other rolesFileContent) rolesFileContent {
	for roleName, roleSpec := range other {
		r[roleName] = roleSpec
	}
	return r
}

// FileBytes returns the file representation of rolesFileContent.
// Since rolesFileContent already corresponds to an unmarshalled yaml representation of the roles files,
// we just marshal it back to yaml.
func (r rolesFileContent) FileBytes() ([]byte, error) {
	return yaml.Marshal(&r)
}
