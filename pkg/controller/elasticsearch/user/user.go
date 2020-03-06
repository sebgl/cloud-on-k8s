// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package user

import (
	"fmt"

	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/client"
)

// user is a convenience struct to represent a file realm user.
type user struct {
	Name         string
	Password     []byte
	PasswordHash []byte
	Roles        []string
}

// FileRealm builds a filerealm representation of this user.
func (u user) FileRealm() fileRealm {
	usersRoles := make(roleUsersMapping, len(u.Roles))
	for _, role := range u.Roles {
		usersRoles[role] = []string{u.Name}
	}
	return fileRealm{
		Users: usersPasswordHashes{
			u.Name: u.PasswordHash,
		},
		UsersRoles: usersRoles,
	}
}

// users is just a list of users to which we attach convenience functions.
type users []user

// FileRealm builds a filerealm representation of the users.
func (users users) FileRealm() fileRealm {
	fileRealm := newFileRealm()
	for _, u := range users {
		fileRealm = fileRealm.MergeWith(u.FileRealm())
	}
	return fileRealm
}

// UserAuth returns an Elasticsearch UserAuth struct for the given user.
func (users users) UserAuth(userName string) (client.UserAuth, error) {
	for _, u := range users {
		if u.Name == userName {
			return client.UserAuth{Name: userName, Password: string(u.Password)}, nil
		}
	}
	return client.UserAuth{}, fmt.Errorf("user %s not found", userName)
}
