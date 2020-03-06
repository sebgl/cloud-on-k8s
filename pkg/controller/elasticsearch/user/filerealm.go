// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package user

import (
	"fmt"
	"sort"
	"strings"

	"github.com/elastic/cloud-on-k8s/pkg/utils/set"
)

const (
	// ElasticUsersFile is the name of the users file in the ES config dir.
	ElasticUsersFile = "users"
	// ElasticUsersRolesFile is the name of the users_roles file in the ES config dir.
	ElasticUsersRolesFile = "users_roles"
)

// fileRealm internal representation, containing user password hashes and role mapping
type fileRealm struct {
	Users      usersPasswordHashes
	UsersRoles roleUsersMapping
}

func newFileRealm() fileRealm {
	return fileRealm{
		Users:      make(usersPasswordHashes),
		UsersRoles: make(roleUsersMapping),
	}
}

func (f fileRealm) MergeWith(others ...fileRealm) fileRealm {
	for _, other := range others {
		f.Users = f.Users.MergeWith(other.Users)
		f.UsersRoles = f.UsersRoles.MergeWith(other.UsersRoles)
	}
	return f
}

func (f fileRealm) PasswordHashForUser(userName string) []byte {
	return f.Users[userName]
}

// usersPasswordHashes is a map {username -> user password hash}
type usersPasswordHashes map[string][]byte

func (u usersPasswordHashes) MergeWith(other usersPasswordHashes) usersPasswordHashes {
	for user, hash := range other {
		u[user] = hash
	}
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

func sortStringSlice(s []string) {
	sort.SliceStable(s, func(i, j int) bool {
		return s[i] < s[j]
	})
}
