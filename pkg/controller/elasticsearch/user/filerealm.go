// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package user

import (
	"fmt"
	"sort"
	"strings"

	"github.com/elastic/cloud-on-k8s/pkg/controller/common/user"
	"github.com/elastic/cloud-on-k8s/pkg/utils/maps"
	"github.com/elastic/cloud-on-k8s/pkg/utils/set"
)

const (
	// ElasticUsersFile is the name of the users file in the ES config dir.
	ElasticUsersFile = "users"
	// ElasticUsersRolesFile is the name of the users_roles file in the ES config dir.
	ElasticUsersRolesFile = "users_roles"
)

type FileRealm struct {
	Users      usersPasswordHashes
	UsersRoles roleUsersMapping
}

func (f FileRealm) MergeWith(other FileRealm) FileRealm {
	return FileRealm{
		Users:      f.Users.MergeWith(other.Users),
		UsersRoles: f.UsersRoles.MergeWith(other.UsersRoles),
	}
}

func FileRealmFromUsers(users []user.User) (FileRealm, error) {
	fileRealm := FileRealm{
		Users:      make(usersPasswordHashes),
		UsersRoles: make(roleUsersMapping),
	}
	for _, user := range users {
		passwordHash, err := user.PasswordHash()
		if err != nil {
			return FileRealm{}, err
		}
		fileRealm.Users = fileRealm.Users.With(user.Id(), string(passwordHash))
		for _, role := range user.Roles() {
			fileRealm.UsersRoles = fileRealm.UsersRoles.With(role, []string{user.Id()})
		}
	}
	return fileRealm, nil
}

// usersPasswordHashes is a map {username -> user password hash}
type usersPasswordHashes map[string]string

func (u usersPasswordHashes) MergeWith(other usersPasswordHashes) usersPasswordHashes {
	return maps.Merge(u, other)
}

func (u usersPasswordHashes) With(userName string, passwordHash string) usersPasswordHashes {
	u[userName] = passwordHash
	return u
}

func (u usersPasswordHashes) FileBytes() []byte {
	rows := make([]string, 0, len(u))
	for user, hash := range u {
		rows = append(rows, fmt.Sprintf("%s:%s", user, hash))
	}
	// sort for consistent comparison
	sortStringSlice(rows)
	return []byte(strings.Join(rows, "\n") + "\n")
}

// roleUsersMapping is a map {role name -> [] user names}
type roleUsersMapping map[string][]string

func (r roleUsersMapping) FileBytes() []byte {
	rows := make([]string, 0, len(r))
	for role, users := range r {
		sortStringSlice(users)
		rows = append(rows, fmt.Sprintf("%s:%s", role, strings.Join(users, ",")))
	}
	// sort for consistent comparison
	sortStringSlice(rows)
	return []byte(strings.Join(rows, "\n") + "\n")
}

func (r roleUsersMapping) MergeWith(other roleUsersMapping) roleUsersMapping {
	if len(other) == 0 {
		return r
	}
	for otherRole, otherUsers := range other {
		currentUsers, exists := r[otherRole]
		if !exists {
			// role does not exist yet, create it
			r[otherRole] = otherUsers
			continue
		}
		// role already exists, merge users
		userSet := set.Make(currentUsers...)
		userSet.MergeWith(set.Make(otherUsers...))
		r[otherRole] = userSet.AsSlice()
	}
	return r
}

func (r roleUsersMapping) With(role string, users []string) roleUsersMapping {
	return r.MergeWith(roleUsersMapping{role: users})
}

func sortStringSlice(s []string) {
	sort.SliceStable(s, func(i, j int) bool {
		return s[i] < s[j]
	})
}
