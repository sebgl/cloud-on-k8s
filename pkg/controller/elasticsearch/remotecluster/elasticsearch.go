// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package remotecluster

import (
	"context"

	esv1 "github.com/elastic/cloud-on-k8s/pkg/apis/elasticsearch/v1"
	"github.com/elastic/cloud-on-k8s/pkg/controller/common/events"
	"github.com/elastic/cloud-on-k8s/pkg/controller/common/hash"
	"github.com/elastic/cloud-on-k8s/pkg/controller/common/license"
	"github.com/elastic/cloud-on-k8s/pkg/controller/common/tracing"
	esclient "github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/client"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/services"
	"github.com/elastic/cloud-on-k8s/pkg/utils/k8s"
	"go.elastic.co/apm"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("remotecluster")

const enterpriseFeaturesDisabledMsg = "Remote cluster is an enterprise feature. Enterprise features are disabled"

type RemoteClustersByName map[string]esclient.RemoteCluster

func (r RemoteClustersByName) Hashes() RemoteClustersHashes {
	hashes := make(RemoteClustersHashes, len(r))
	for name, remote := range r {
		hashes[name] = remoteClusterHash(remote)
	}
	return hashes
}
func remoteClusterHash(remote esclient.RemoteCluster) string {
	return hash.HashObject(remote)
}

func ParseRemoteClusters(es esv1.Elasticsearch) RemoteClustersByName {
	output := make(RemoteClustersByName)
	for _, remoteSpec := range es.Spec.RemoteClusters {
		var seeds []string
		switch {
		case remoteSpec.ElasticsearchRef.IsDefined():
			esKey := remoteSpec.ElasticsearchRef.WithDefaultNamespace(es.Namespace)
			seeds = []string{services.ExternalTransportServiceHost(esKey.NamespacedName())}
		default:
			continue
		}
		output[remoteSpec.Name] = esclient.RemoteCluster{Seeds: seeds}
	}
	return output
}

// UpdateSettings updates the remote clusters in the persistent settings by calling the Elasticsearch API.
func UpdateSettings(
	ctx context.Context,
	c k8s.Client,
	esClient esclient.Client,
	eventRecorder record.EventRecorder,
	licenseChecker license.Checker,
	es esv1.Elasticsearch,
) error {
	span, _ := apm.StartSpan(ctx, "update_remote_clusters", tracing.SpanTypeApp)
	defer span.End()

	enabled, err := licenseChecker.EnterpriseFeaturesEnabled()
	if err != nil {
		return err
	}
	if !enabled {
		log.Info(
			enterpriseFeaturesDisabledMsg,
			"namespace", es.Namespace, "es_name", es.Name,
		)
		eventRecorder.Eventf(&es, corev1.EventTypeWarning, events.EventAssociationError, enterpriseFeaturesDisabledMsg)
		return nil
	}

	actual, err := remoteClusterHashesFromAnnotation(es)
	if err != nil {
		return err
	}
	expected := ParseRemoteClusters(es)

	toUpdate := make(map[string]esclient.RemoteCluster)
	// RemoteClusters to add or update
	for name, remoteCluster := range expected {
		if !actual.Contains(name, remoteClusterHash(remoteCluster)) {
			// Declare remote cluster in ES
			log.Info("Adding or updating remote cluster",
				"namespace", es.Namespace,
				"es_name", es.Name,
				"remote_cluster", name,
				"seeds", remoteCluster.Seeds,
			)
			toUpdate[name] = remoteCluster
		}
	}

	// RemoteClusters to remove
	for name := range actual {
		if _, ok := expected[name]; !ok {
			log.Info("Removing remote cluster",
				"namespace", es.Namespace,
				"es_name", es.Name,
				"remote_cluster", name,
			)
			// set the seeds to nil to remove the remote cluster entry
			toUpdate[name] = esclient.RemoteCluster{Seeds: nil}
		}
	}

	if len(toUpdate) > 0 {
		// Apply the settings
		if err := updateSettings(esClient, toUpdate); err != nil {
			return err
		}
		// Update the annotation
		return annotateWithRemoteClusters(c, es, expected.Hashes())
	}
	return nil
}

// updateSettings makes a call to an Elasticsearch cluster to apply a persistent setting.
func updateSettings(esClient esclient.Client, remoteClusters map[string]esclient.RemoteCluster) error {
	ctx, cancel := context.WithTimeout(context.Background(), esclient.DefaultReqTimeout)
	defer cancel()
	return esClient.UpdateSettings(ctx, esclient.Settings{
		PersistentSettings: &esclient.SettingsGroup{
			Cluster: esclient.Cluster{
				RemoteClusters: remoteClusters,
			},
		},
	})
}
