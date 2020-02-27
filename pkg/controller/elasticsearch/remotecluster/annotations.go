// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package remotecluster

import (
	"encoding/json"

	esv1 "github.com/elastic/cloud-on-k8s/pkg/apis/elasticsearch/v1"
	"github.com/elastic/cloud-on-k8s/pkg/utils/k8s"
	"github.com/pkg/errors"
)

const (
	RemoteClustersAnnotationName = "elasticsearch.k8s.elastic.co/remote-clusters"
)

// RemoteClusterHashes is a map {remote cluster name -> remote cluster spec hash}
type RemoteClustersHashes map[string]string

func (r RemoteClustersHashes) Contains(name string, hash string) bool {
	if actualHash, ok := r[name]; ok && actualHash == hash {
		return true
	}
	return false
}

// remoteClusterHashesFromAnnotation returns the configuration hashes of the remote clusters declared in Elasticsearch.
func remoteClusterHashesFromAnnotation(es esv1.Elasticsearch) (RemoteClustersHashes, error) {
	output := RemoteClustersHashes{}
	return output, json.Unmarshal([]byte(es.Annotations[RemoteClustersAnnotationName]), &output)
}

func annotateWithRemoteClusters(c k8s.Client, es esv1.Elasticsearch, remoteClusters RemoteClustersHashes) error {
	// serialize the remote clusters list and update the object
	serializedRemoteClusters, err := json.Marshal(remoteClusters)
	if err != nil {
		return errors.Wrapf(err, "failed to serialize remote cluster")
	}
	if es.Annotations == nil {
		es.Annotations = make(map[string]string)
	}
	es.Annotations[RemoteClustersAnnotationName] = string(serializedRemoteClusters)
	return c.Update(&es)
}
