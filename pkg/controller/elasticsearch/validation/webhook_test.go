// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package validation

import (
	"testing"

	commonv1 "github.com/elastic/cloud-on-k8s/pkg/apis/common/v1"

	"github.com/stretchr/testify/require"

	esv1 "github.com/elastic/cloud-on-k8s/pkg/apis/elasticsearch/v1"
	v1 "github.com/elastic/cloud-on-k8s/pkg/apis/elasticsearch/v1"
)

func Test_validatingWebhook_validateCreate(t *testing.T) {
	tests := []struct {
		name    string
		es      v1.Elasticsearch
		wantErr string
	}{
		{
			name: "should run creation checks (invalid version)",
			es: esv1.Elasticsearch{
				Spec: esv1.ElasticsearchSpec{
					Version:  "x.y",
					NodeSets: []v1.NodeSet{{Count: 1}},
				},
			},
			wantErr: parseVersionErrMsg,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wh := &validatingWebhook{}
			err := wh.validateCreate(tt.es)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func Test_validatingWebhook_validateUpdate(t *testing.T) {
	type args struct {
		old v1.Elasticsearch
		new v1.Elasticsearch
	}
	tests := []struct {
		name    string
		args    args
		wantErr string
	}{
		{
			name: "should run update checks (version downgrade)",
			args: args{
				old: esv1.Elasticsearch{
					Spec: esv1.ElasticsearchSpec{
						Version:  "7.9.0",
						NodeSets: []v1.NodeSet{{Count: 1}},
					},
				},
				new: esv1.Elasticsearch{
					Spec: esv1.ElasticsearchSpec{
						Version:  "7.8.0",
						NodeSets: []v1.NodeSet{{Count: 1}},
					},
				},
			},
			wantErr: noDowngradesMsg,
		},
		{
			name: "should also run creation checks (no master node)",
			args: args{
				old: esv1.Elasticsearch{
					Spec: esv1.ElasticsearchSpec{
						Version:  "7.9.0",
						NodeSets: []v1.NodeSet{{Count: 1}},
					},
				},
				new: esv1.Elasticsearch{
					Spec: esv1.ElasticsearchSpec{
						Version: "7.9.0",
						NodeSets: []v1.NodeSet{{Count: 1, Config: &commonv1.Config{Data: map[string]interface{}{
							"node.master": false,
						}}}},
					},
				},
			},
			wantErr: masterRequiredMsg,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wh := &validatingWebhook{}
			err := wh.validateUpdate(tt.args.old, tt.args.new)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
