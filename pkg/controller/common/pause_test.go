// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type testcase struct {
	name string

	// annotationSequence is list of annotations that are simulated.
	annotationSequence []map[string]string

	// Expected pause status.
	expectedState []bool
}

func TestPauseCondition(t *testing.T) {
	tests := []testcase{
		{
			name: "Simple pause/resume simulation (a.k.a the Happy Path)",
			annotationSequence: []map[string]string{
				{PauseAnnotationName: "true"},
				{PauseAnnotationName: "false"},
				{PauseAnnotationName: "true"},
				{PauseAnnotationName: "false"},
			},
			expectedState: []bool{
				true,
				false,
				true,
				false,
			},
		},
		{
			name: "Can't parse or empty annotation",
			annotationSequence: []map[string]string{
				{PauseAnnotationName: ""}, // empty annotation
				{PauseAnnotationName: "true"},
				{PauseAnnotationName: "XXXX"}, // unable to parse this one
				{PauseAnnotationName: "1"},    // 1 == true
				{PauseAnnotationName: "0"},    // 0 == false
			},
			expectedState: []bool{
				false,
				true,
				false,
				true,
				false,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for i, expectedState := range test.expectedState {
				obj := corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "bar",
						Namespace:   "foo",
						Annotations: test.annotationSequence[i],
					},
				}
				actualPauseState := IsPaused(&obj)
				assert.Equal(t, expectedState, actualPauseState)
			}
		})
	}
}
