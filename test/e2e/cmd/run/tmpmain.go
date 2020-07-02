// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package run

import (
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/elastic/cloud-on-k8s/test/e2e/test"
)

func TmpMain() {
	log = logf.Log.WithName("tmp")
	namespace := "default"
	h := helper{
		testContext: test.Context{
			E2ENamespace: namespace,
			TestRun:      "tmp",
		},
	}
	client, err := h.createKubeClient()
	if err != nil {
		panic(err)
	}
	if err := h.monitorTestJob(client); err != nil {
		panic(err)
	}
}
