// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package user

import (
	"testing"

	"github.com/stretchr/testify/require"

	esclient "github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/client"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/user/filerealm"
)

func Test_user_FileRealm(t *testing.T) {
	user := user{
		Name:         "user1",
		Password:     []byte("password1"),
		PasswordHash: []byte("password1Hash"),
		Roles:        []string{"role1", "role2"},
	}
	expected := filerealm.New().
		WithUser("user1", []byte("password1Hash")).
		WithRole("role1", []string{"user1"}).
		WithRole("role2", []string{"user1"})

	require.Equal(t, expected, user.fileRealm())
}

func Test_users_FileRealm(t *testing.T) {
	users := users{
		{
			Name:         "user1",
			Password:     []byte("password1"),
			PasswordHash: []byte("password1Hash"),
			Roles:        []string{"role1", "role2"},
		},
		{
			Name:         "user2",
			Password:     []byte("password2"),
			PasswordHash: []byte("password2Hash"),
			Roles:        []string{"role2", "role3"},
		},
	}
	expected := filerealm.New().
		WithUser("user1", []byte("password1Hash")).
		WithUser("user2", []byte("password2Hash")).
		WithRole("role1", []string{"user1"}).
		WithRole("role2", []string{"user1", "user2"}).
		WithRole("role3", []string{"user2"})

	require.Equal(t, expected, users.fileRealm())
}

func Test_users_UserAuth(t *testing.T) {
	users := users{
		{
			Name:         "user1",
			Password:     []byte("password1"),
			PasswordHash: []byte("password1Hash"),
			Roles:        []string{"role1", "role2"},
		},
		{
			Name:         "user2",
			Password:     []byte("password2"),
			PasswordHash: []byte("password2Hash"),
			Roles:        []string{"role2", "role3"},
		},
	}

	auth, err := users.userAuth("user1")
	require.NoError(t, err)
	require.Equal(t, esclient.UserAuth{
		Name:     "user1",
		Password: "password1",
	}, auth)

	// non-existing user should return an error
	_, err = users.userAuth("unknown-user")
	require.Error(t, err, "user unknown-user not found")
}
