// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp" // allow gcp authentication
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	esv1 "github.com/elastic/cloud-on-k8s/pkg/apis/elasticsearch/v1"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/sset"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/volume"
	"github.com/elastic/cloud-on-k8s/pkg/utils/k8s"
)

const (
	esManifestFlag      = "elasticsearch-manifest"
	dryRunFlag          = "dry-run"
	pvBackupFlag        = "pv-backup-path"
	defaultPVBackupPath = "pv_backup_{timestamp}.json"
)

var Cmd = &cobra.Command{
	Use:   "reattach-pv",
	Short: "Recreate an Elasticsearch cluster by reattaching existing released PersistentVolumes",
	Run: func(cmd *cobra.Command, args []string) {
		dryRun := viper.GetBool(dryRunFlag)
		if dryRun {
			fmt.Println("Running in dry run mode")
		}

		err := esv1.AddToScheme(scheme.Scheme)
		exitOnErr(err)

		es, err := esFromFile(viper.GetString(esManifestFlag))
		exitOnErr(err)

		c, err := createClient()
		exitOnErr(err)

		err = checkElasticsearchNotFound(c, es)
		exitOnErr(err)

		expectedClaims := expectedVolumeClaims(es)
		err = checkClaimsNotFound(c, expectedClaims)
		exitOnErr(err)

		releasedPVs, err := findReleasedPVs(c, es)
		exitOnErr(err)

		matches, err := matchPVsWithClaim(releasedPVs, expectedClaims)
		exitOnErr(err)

		err = backupPVs(matches, pvBackupFilepath(viper.GetString(pvBackupFlag)))
		exitOnErr(err)

		err = createAndBindClaims(c, matches, dryRun)
		exitOnErr(err)

		es, err = createElasticsearch(c, es, dryRun)
		exitOnErr(err)
	},
}

func init() {
	Cmd.Flags().String(
		esManifestFlag,
		"",
		"path pointing to the Elasticsearch yaml manifest",
	)
	Cmd.Flags().Bool(
		dryRunFlag,
		false,
		"do not apply any Kubernetes resource change",
	)
	Cmd.Flags().String(
		pvBackupFlag,
		defaultPVBackupPath,
		"path to the file where a backup of existing PersistentVolumes will be stored before update, set empty to disable",
	)
	exitOnErr(viper.BindPFlags(Cmd.Flags()))
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
}

func main() {
	exitOnErr(Cmd.Execute())
}

// esFromFile parses an Elasticsearch resource from the given yaml manifest path.
func esFromFile(path string) (esv1.Elasticsearch, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return esv1.Elasticsearch{}, err
	}
	obj, _, err := scheme.Codecs.UniversalDeserializer().Decode(data, nil, nil)
	if err != nil {
		return esv1.Elasticsearch{}, nil
	}
	es := *obj.(*esv1.Elasticsearch)
	if es.Namespace == "" {
		fmt.Println("Setting Elasticsearch namespace to 'default'")
		es.Namespace = "default"
	}
	return es, nil
}

// createClient creates a Kubernetes client targeting the current default K8s cluster.
func createClient() (k8s.Client, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		return nil, err
	}
	return k8s.WrapClient(c), nil
}

// checkElasticsearchNotFound returns an error if the given Elasticsearch resource already exists.
func checkElasticsearchNotFound(c k8s.Client, es esv1.Elasticsearch) error {
	var retrieved esv1.Elasticsearch
	err := c.Get(k8s.ExtractNamespacedName(&es), &retrieved)
	if err == nil {
		return fmt.Errorf("elasticsearch resource %s exists in the apiserver: it should be deleted first", es.Name)
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// checkClaimsNotFound returns an error if the given PersistentVolumeClaims already exist.
func checkClaimsNotFound(c k8s.Client, claims map[types.NamespacedName]v1.PersistentVolumeClaim) error {
	for nsn := range claims {
		err := c.Get(nsn, &v1.PersistentVolumeClaim{})
		if err == nil {
			return fmt.Errorf("PersistentVolumeClaim %s seems to exist in the apiserver", nsn)
		}
		if !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// expectedVolumeClaims builds the list of PersistentVolumeClaim that we expect to exist for the given
// Elasticsearch cluster.
func expectedVolumeClaims(es esv1.Elasticsearch) map[types.NamespacedName]v1.PersistentVolumeClaim {
	claims := make(map[types.NamespacedName]v1.PersistentVolumeClaim, es.Spec.NodeCount())
	for _, nodeSet := range es.Spec.NodeSets {
		for i := int32(0); i < nodeSet.Count; i++ {
			var claim v1.PersistentVolumeClaim
			for _, claimTemplate := range nodeSet.VolumeClaimTemplates {
				if claimTemplate.Name == volume.ElasticsearchDataVolumeName {
					claim = claimTemplate
				}
			}
			claim.Name = fmt.Sprintf(
				"%s-%s",
				volume.ElasticsearchDataVolumeName,
				sset.PodName(esv1.StatefulSet(es.Name, nodeSet.Name), i))
			claim.Namespace = es.Namespace
			if claim.Namespace == "" {
				claim.Namespace = "default"
			}
			// simulate a bound status
			claim.Status = v1.PersistentVolumeClaimStatus{
				Phase:       v1.ClaimBound,
				AccessModes: claim.Spec.AccessModes,
				Capacity:    claim.Spec.Resources.Requests,
			}
			claims[types.NamespacedName{Namespace: es.Namespace, Name: claim.Name}] = claim
			fmt.Printf("Expecting claim %s\n", claim.Name)
		}
	}
	return claims
}

// findReleasedPVs returns the list of Released PersistentVolumes.
func findReleasedPVs(c k8s.Client, es esv1.Elasticsearch) ([]v1.PersistentVolume, error) {
	var pvs v1.PersistentVolumeList
	if err := c.List(&pvs); err != nil {
		return nil, err
	}
	var released []v1.PersistentVolume
	for _, pv := range pvs.Items {
		if pv.Status.Phase == v1.VolumeReleased {
			released = append(released, pv)
		}
	}
	fmt.Printf("Found %d released PersistentVolumes\n", len(pvs.Items))
	return pvs.Items, nil
}

// pvBackupFilepath injects a timestamp in the default PV backup file.
func pvBackupFilepath(flagValue string) string {
	if flagValue == defaultPVBackupPath {
		// set the current timestamp
		return strings.Replace(flagValue, "{timestamp}", fmt.Sprintf("%d", time.Now().Unix()), 1)
	}
	return flagValue
}

// backupPVs stores a JSON backup of the given PersistentVolumes in toFile.
func backupPVs(matches []MatchingVolumeClaim, toFile string) error {
	if toFile == "" {
		fmt.Println("Skipping PV backup file creation")
		return nil
	}
	pvs := make([]v1.PersistentVolume, 0, len(matches))
	for _, match := range matches {
		pvs = append(pvs, match.volume)
	}
	asJson, err := json.Marshal(pvs)
	if err != nil {
		return err
	}
	fmt.Printf("Creating a backup of released PersistentVolumes in %s\n", toFile)
	return ioutil.WriteFile(toFile, asJson, 0644)
}

// MatchingVolumeClaim matches an existing PersistentVolume with a new PersistentVolumeClaim.
type MatchingVolumeClaim struct {
	claim  v1.PersistentVolumeClaim
	volume v1.PersistentVolume
}

// matchPVsWithClaim iterates over existing pvs to match them to an expected pvc.
func matchPVsWithClaim(pvs []v1.PersistentVolume, claims map[types.NamespacedName]v1.PersistentVolumeClaim) ([]MatchingVolumeClaim, error) {
	var matches []MatchingVolumeClaim
	for _, pv := range pvs {
		if pv.Spec.ClaimRef == nil {
			continue
		}
		claim, expected := claims[types.NamespacedName{Namespace: pv.Spec.ClaimRef.Namespace, Name: pv.Spec.ClaimRef.Name}]
		if !expected {
			continue
		}
		fmt.Printf("Found matching volume %s for claim %s\n", pv.Name, claim.Name)
		matches = append(matches, MatchingVolumeClaim{
			claim:  claim,
			volume: pv,
		})
	}
	if len(matches) != len(claims) {
		return nil, fmt.Errorf("found %d matching volumes but expected %d", len(matches), len(claims))
	}
	return matches, nil
}

// bindNewClaims creates the given PersistentVolumeClaims, and update the matching PersistentVolumes
// to reference the claim.
func createAndBindClaims(c k8s.Client, volumeClaims []MatchingVolumeClaim, dryRun bool) error {
	for _, match := range volumeClaims {
		fmt.Printf("Creating claim %s\n", match.claim.Name)
		if !dryRun {
			if err := c.Create(&match.claim); err != nil {
				return err
			}
		}
		fmt.Printf("Updating volume %s to reference claim %s\n", match.volume.Name, match.claim.Name)
		// match.claim now stores the created claim metadata
		// patch the volume spec to match the new claim
		match.volume.Spec.ClaimRef.UID = match.claim.UID
		match.volume.Spec.ClaimRef.ResourceVersion = match.claim.ResourceVersion
		if !dryRun {
			if err := c.Update(&match.volume); err != nil {
				return err
			}
		}
	}
	return nil
}

// createElasticsearch creates the given Elasticsearch resource.
func createElasticsearch(c k8s.Client, es esv1.Elasticsearch, dryRun bool) (esv1.Elasticsearch, error) {
	fmt.Printf("Creating Elasticsearch %s\n", es.Name)
	if dryRun {
		return es, nil
	}
	return es, c.Create(&es, &client.CreateOptions{})
}

// exitOnErr prints the given error then exits with status code 1
func exitOnErr(err error) {
	if err != nil {
		println("Fatal error:", err.Error())
		os.Exit(1)
	}
}
