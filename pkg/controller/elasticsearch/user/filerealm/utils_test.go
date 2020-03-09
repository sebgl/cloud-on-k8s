// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package filerealm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_sortStringSlice(t *testing.T) {
	slice := []string{"aab", "aac", "aaa", "aab"}
	sortStringSlice(slice)
	require.Equal(t, []string{"aaa", "aab", "aab", "aac"}, slice)
}
