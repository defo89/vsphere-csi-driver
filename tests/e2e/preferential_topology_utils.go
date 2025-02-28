/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"context"
	"fmt"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/vmware/govmomi/object"
	"golang.org/x/crypto/ssh"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/test/e2e/framework"
	fssh "k8s.io/kubernetes/test/e2e/framework/ssh"
	fss "k8s.io/kubernetes/test/e2e/framework/statefulset"
	"k8s.io/utils/strings/slices"
)

var sshClientConfig *ssh.ClientConfig = &ssh.ClientConfig{
	User: "root",
	Auth: []ssh.AuthMethod{
		ssh.Password(k8sVmPasswd),
	},
	HostKeyCallback: ssh.InsecureIgnoreHostKey(),
}

// govc login cmd
func govcLoginCmd() string {
	loginCmd := "export GOVC_INSECURE=1;"
	loginCmd += fmt.Sprintf("export GOVC_URL='https://%s:%s@%s:%s';",
		e2eVSphere.Config.Global.User, e2eVSphere.Config.Global.Password,
		e2eVSphere.Config.Global.VCenterHostname, e2eVSphere.Config.Global.VCenterPort)
	return loginCmd
}

/*
getTopologyLevel5ClusterGroupNames method is used to fetch list of cluster available
in level-5 testbed
*/
func getTopologyLevel5ClusterGroupNames(masterIp string,
	dataCenter []*object.Datacenter) ([]string, error) {
	var clusterList, clusList, clusFolderTemp, clusterGroupRes []string
	var clusterFolderName string
	for i := 0; i < len(dataCenter); i++ {
		clusterFolder := govcLoginCmd() + "govc ls " + dataCenter[i].InventoryPath
		framework.Logf("cmd: %s ", clusterFolder)
		clusterFolderNameResult, err := sshExec(sshClientConfig, masterIp, clusterFolder)
		if err != nil && clusterFolderNameResult.Code != 0 {
			fssh.LogResult(clusterFolderNameResult)
			return nil, fmt.Errorf("couldn't execute command: %s on host: %v , error: %s",
				clusterFolder, masterIp, err)
		}
		if clusterFolderNameResult.Stdout != "" {
			clusFolderTemp = strings.Split(clusterFolderNameResult.Stdout, "\n")
		}
		for i := 0; i < len(clusFolderTemp)-1; i++ {
			if strings.Contains(clusFolderTemp[i], "host") {
				clusterFolderName = clusFolderTemp[i]
				break
			}
		}
		clusterGroup := govcLoginCmd() + "govc ls " + clusterFolderName
		framework.Logf("cmd: %s ", clusterGroup)
		clusterGroupResult, err := sshExec(sshClientConfig, masterIp, clusterGroup)
		if err != nil && clusterGroupResult.Code != 0 {
			fssh.LogResult(clusterGroupResult)
			return nil, fmt.Errorf("couldn't execute command: %s on host: %v , error: %s",
				clusterGroup, masterIp, err)
		}
		if clusterGroupResult.Stdout != "" {
			clusterGroupRes = strings.Split(clusterGroupResult.Stdout, "\n")
		}
		cluster := govcLoginCmd() + "govc ls " + clusterGroupRes[0] + " | sort"
		framework.Logf("cmd: %s ", cluster)
		clusterResult, err := sshExec(sshClientConfig, masterIp, cluster)
		if err != nil && clusterResult.Code != 0 {
			fssh.LogResult(clusterResult)
			return nil, fmt.Errorf("couldn't execute command: %s on host: %v , error: %s",
				cluster, masterIp, err)
		}
		if clusterResult.Stdout != "" {
			clusListTemp := strings.Split(clusterResult.Stdout, "\n")
			clusList = append(clusList, clusListTemp...)
		}
		for i := 0; i < len(clusList)-1; i++ {
			clusterList = append(clusterList, clusList[i])
		}
		clusList = nil
	}
	return clusterList, nil
}

/*
attachTagToPreferredDatastore method is used to attach the  preferred tag to the
datastore chosen for volume provisioning
*/
func attachTagToPreferredDatastore(masterIp string, datastore string, tagName string) error {
	attachTagCat := govcLoginCmd() +
		"govc tags.attach -c " + preferredDSCat + " " + tagName + " " + "'" + datastore + "'"
	framework.Logf("cmd to attach tag to preferred datastore: %s ", attachTagCat)
	attachTagCatRes, err := sshExec(sshClientConfig, masterIp, attachTagCat)
	if err != nil && attachTagCatRes.Code != 0 {
		fssh.LogResult(attachTagCatRes)
		return fmt.Errorf("couldn't execute command: %s on host: %v , error: %s",
			attachTagCat, masterIp, err)
	}
	return nil
}

/* detachTagCreatedOnPreferredDatastore is used to detach the tag created on preferred datastore */
func detachTagCreatedOnPreferredDatastore(masterIp string, datastore string, tagName string) error {
	detachTagCat := govcLoginCmd() +
		"govc tags.detach -c " + preferredDSCat + " " + tagName + " " + "'" + datastore + "'"
	framework.Logf("cmd to detach the tag assigned to preferred datastore: %s ", detachTagCat)
	detachTagCatRes, err := sshExec(sshClientConfig, masterIp, detachTagCat)
	if err != nil && detachTagCatRes.Code != 0 {
		fssh.LogResult(detachTagCatRes)
		return fmt.Errorf("couldn't execute command: %s on host: %v , error: %s",
			detachTagCat, masterIp, err)
	}
	return nil
}

/*
getListOfSharedDatastoresBetweenVMs method is used to fetch the list of datatsores shared between
node vms or shared across entire k8s cluster
*/
func getListOfSharedDatastoresBetweenVMs(masterIp string,
	dataCenter []*object.Datacenter) (map[string]string, error) {
	var clusFolderTemp []string
	var clusterFolderName string
	shareddatastoreListMap := make(map[string]string)
	for i := 0; i < len(dataCenter); i++ {
		clusterFolder := govcLoginCmd() + "govc ls " + dataCenter[i].InventoryPath
		framework.Logf("cmd: %s ", clusterFolder)
		clusterFolderNameResult, err := sshExec(sshClientConfig, masterIp, clusterFolder)
		if err != nil && clusterFolderNameResult.Code != 0 {
			fssh.LogResult(clusterFolderNameResult)
			return nil, fmt.Errorf("couldn't execute command: %s on host: %v , error: %s",
				clusterFolder, masterIp, err)
		}
		if clusterFolderNameResult.Stdout != "" {
			clusFolderTemp = strings.Split(clusterFolderNameResult.Stdout, "\n")
		}
		for i := 0; i < len(clusFolderTemp)-1; i++ {
			if strings.Contains(clusFolderTemp[i], "vm") {
				clusterFolderName = clusFolderTemp[i]
				break
			}
		}
	}
	listOfSharedDatastores := govcLoginCmd() +
		"govc ls " + clusterFolderName + " | xargs -n1 -I% govc object.collect -s % summary.runtime.host | " +
		"xargs govc datastore.info -H | grep 'Path\\|URL' | tr -s [:space:]"
	framework.Logf("cmd for fetching list of shared datastores: %s ", listOfSharedDatastores)
	result, err := sshExec(sshClientConfig, masterIp, listOfSharedDatastores)
	if err != nil && result.Code != 0 {
		fssh.LogResult(result)
		return nil, fmt.Errorf("couldn't execute command: %s on host: %v , error: %s",
			listOfSharedDatastores, masterIp, err)
	}
	sharedDatastoreList := strings.Split(result.Stdout, "\n")
	for i := 0; i < len(sharedDatastoreList)-1; i = i + 2 {
		key := strings.ReplaceAll(sharedDatastoreList[i], " Path: ", "")
		value := strings.ReplaceAll(sharedDatastoreList[i+1], " URL: ", "")
		shareddatastoreListMap[key] = value
	}
	return shareddatastoreListMap, nil
}

/*
getListOfDatastoresByClusterName method is used to fetch the list of datastores accessible to
specific cluster
*/
func getListOfDatastoresByClusterName(masterIp string, cluster string) (map[string]string, error) {
	ClusterdatastoreListMap := make(map[string]string)
	datastoreListByCluster := govcLoginCmd() +
		"govc object.collect -s -d ' ' " + cluster + " host | xargs govc datastore.info -H | " +
		"grep 'Path\\|URL' | tr -s [:space:]"
	framework.Logf("cmd : %s ", datastoreListByCluster)
	result, err := sshExec(sshClientConfig, masterIp, datastoreListByCluster)
	if err != nil && result.Code != 0 {
		fssh.LogResult(result)
		return nil, fmt.Errorf("couldn't execute command: %s on host: %v , error: %s",
			datastoreListByCluster, masterIp, err)
	}
	datastoreList := strings.Split(result.Stdout, "\n")
	for i := 0; i < len(datastoreList)-1; i = i + 2 {
		key := strings.ReplaceAll(datastoreList[i], " Path: ", "")
		value := strings.ReplaceAll(datastoreList[i+1], " URL: ", "")
		ClusterdatastoreListMap[key] = value
	}

	return ClusterdatastoreListMap, nil
}

/*
verifyVolumeProvisioningForStatefulSet is used to check whether the volume is provisioned on the
chosen preferred datastore or not for statefulsets
*/
func verifyVolumeProvisioningForStatefulSet(ctx context.Context,
	client clientset.Interface, statefulset *appsv1.StatefulSet,
	namespace string, datastoreNames []string, datastoreListMap map[string]string,
	multipleAllowedTopology bool, parallelStatefulSetCreation bool) error {
	counter := 0
	stsPodCount := 0
	var dsUrls []string
	var ssPodsBeforeScaleDown *v1.PodList
	if parallelStatefulSetCreation {
		ssPodsBeforeScaleDown = GetListOfPodsInSts(client, statefulset)
	} else {
		ssPodsBeforeScaleDown = fss.GetPodList(client, statefulset)
	}
	stsPodCount = len(ssPodsBeforeScaleDown.Items)
	for i := 0; i < len(datastoreNames); i++ {
		if val, ok := datastoreListMap[datastoreNames[i]]; ok {
			dsUrls = append(dsUrls, val)
		}
	}
	for _, sspod := range ssPodsBeforeScaleDown.Items {
		_, err := client.CoreV1().Pods(namespace).Get(ctx, sspod.Name, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		for _, volumespec := range sspod.Spec.Volumes {
			if volumespec.PersistentVolumeClaim != nil {
				pv := getPvFromClaim(client, statefulset.Namespace, volumespec.PersistentVolumeClaim.ClaimName)
				isPreferred := e2eVSphere.verifyPreferredDatastoreMatch(pv.Spec.CSI.VolumeHandle, dsUrls)
				if isPreferred {
					framework.Logf("volume %s is created on preferred datastore %v", pv.Spec.CSI.VolumeHandle, dsUrls)
					counter = counter + 1
				}
			}
		}
	}
	if len(datastoreNames) == 1 && counter != stsPodCount && !multipleAllowedTopology {
		return fmt.Errorf("volume is provisioned on the wrong datastore")
	} else if len(datastoreNames) == 2 && counter != stsPodCount && !multipleAllowedTopology {
		return fmt.Errorf("volume is provisioned on the wrong datastore")
	} else if len(datastoreNames) == 3 && counter != stsPodCount && !multipleAllowedTopology {
		return fmt.Errorf("volume is provisioned on the wrong datastore")
	} else if len(datastoreNames) == 4 && counter != stsPodCount && !multipleAllowedTopology {
		return fmt.Errorf("volume is provisioned on the wrong datastore")
	} else if counter != stsPodCount && multipleAllowedTopology && counter != 0 {
		framework.Logf("Few volume is provisioned on some other datastore due to multiple " +
			"allowed topology set or no topology set in the Storage Class")
	}
	return nil
}

/*
verifyVolumeProvisioningForStandalonePods is used to check whether the volume is provisioned on the
chosen preferred datastore or not for standalone pods
*/
func verifyVolumeProvisioningForStandalonePods(ctx context.Context,
	client clientset.Interface, pod *v1.Pod,
	namespace string, datastoreNames []string, datastoreListMap map[string]string) {
	var flag bool = false
	var dsUrls []string
	for i := 0; i < len(datastoreNames); i++ {
		if val, ok := datastoreListMap[datastoreNames[i]]; ok {
			dsUrls = append(dsUrls, val)
		}
	}
	for _, volumespec := range pod.Spec.Volumes {
		if volumespec.PersistentVolumeClaim != nil {
			pv := getPvFromClaim(client, pod.Namespace, volumespec.PersistentVolumeClaim.ClaimName)
			isPreferred := e2eVSphere.verifyPreferredDatastoreMatch(pv.Spec.CSI.VolumeHandle, dsUrls)
			if isPreferred {
				framework.Logf("volume %s is created on preferred datastore %v", pv.Spec.CSI.VolumeHandle, dsUrls)
				flag = true
			}
		}
	}
	if !flag {
		gomega.Expect(flag).To(gomega.BeTrue(), "Volume is not provisioned on the preferred datastore")
	}
}

/*
tagSameDatastoreAsPreferenceToDifferentRacks method is used to assign same preferred datatsore
to another racks or clusters available in a testbed
*/
func tagSameDatastoreAsPreferenceToDifferentRacks(masterIp string, zoneValue string,
	itr int, datastoreNames []string) error {
	i := 0
	for j := 0; j < len(datastoreNames); j++ {
		i = i + 1
		err := attachTagToPreferredDatastore(masterIp, datastoreNames[j], zoneValue)
		if err != nil {
			return err
		}
		if i == itr {
			break
		}
	}
	return nil
}

/*
tagPreferredDatastore method is used to tag the datastore which is chosen for volume provisioning
*/
func tagPreferredDatastore(masterIp string, zoneValue string, itr int,
	datastoreListMap map[string]string, datastoreNames []string) ([]string, error) {
	var preferredDatastorePaths []string
	i := 0
	if datastoreNames == nil {
		for dsName := range datastoreListMap {
			i = i + 1
			preferredDatastorePaths = append(preferredDatastorePaths, dsName)
			err := attachTagToPreferredDatastore(masterIp, dsName, zoneValue)
			if err != nil {
				return preferredDatastorePaths, err
			}
			if i == itr {
				break
			}
		}
	}
	if datastoreNames != nil {
		for dsName := range datastoreListMap {
			if !slices.Contains(datastoreNames, dsName) {
				preferredDatastorePaths = append(preferredDatastorePaths, dsName)
				err := attachTagToPreferredDatastore(masterIp, dsName, zoneValue)
				if err != nil {
					return preferredDatastorePaths, err
				}
				i = i + 1
			}
			if i == itr {
				break
			}
		}
	}
	return preferredDatastorePaths, nil
}

// restartCSIDriver method restarts the csi driver
func restartCSIDriver(ctx context.Context, client clientset.Interface, namespace string,
	csiReplicas int32) (bool, error) {
	isServiceStopped, err := stopCSIPods(ctx, client)
	if err != nil {
		return isServiceStopped, err
	}
	isServiceStarted, err := startCSIPods(ctx, client, csiReplicas)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	if err != nil {
		return isServiceStarted, err
	}
	return true, nil
}

/*
setPreferredDatastoreTimeInterval method is used to set the time interval at which preferred
datastores are refreshed in the environment
*/
func setPreferredDatastoreTimeInterval(client clientset.Interface, ctx context.Context,
	csiNamespace string, namespace string, csiReplicas int32) {
	currentSecret, err := client.CoreV1().Secrets(csiNamespace).Get(ctx, configSecret, metav1.GetOptions{})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	originalConf := string(currentSecret.Data[vSphereCSIConf])
	vsphereCfg, err := readConfigFromSecretString(originalConf)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	vsphereCfg.Global.CSIFetchPreferredDatastoresIntervalInMin = preferredDatastoreRefreshTimeInterval
	modifiedConf, err := writeConfigToSecretString(vsphereCfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	ginkgo.By("Updating the secret to reflect new changes")
	currentSecret.Data[vSphereCSIConf] = []byte(modifiedConf)
	_, err = client.CoreV1().Secrets(csiNamespace).Update(ctx, currentSecret, metav1.UpdateOptions{})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	// restart csi driver
	restartSuccess, err := restartCSIDriver(ctx, client, csiNamespace, csiReplicas)
	gomega.Expect(restartSuccess).To(gomega.BeTrue(), "csi driver restart not successful")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

/*
getNonSharedDatastoresInCluster method is used to fetch the datatsore which is accessible
only to specific cluster and not shared with any other clusters
*/
func getNonSharedDatastoresInCluster(ClusterdatastoreListMap map[string]string,
	shareddatastoreListMap map[string]string) map[string]string {
	NonShareddatastoreListMap := make(map[string]string)
	for ClusterDsName, clusterDsVal := range ClusterdatastoreListMap {
		if _, ok := shareddatastoreListMap[ClusterDsName]; ok {
		} else {
			NonShareddatastoreListMap[ClusterDsName] = clusterDsVal
		}
	}
	return NonShareddatastoreListMap
}

// deleteTagCreatedForPreferredDatastore method is used to delete the tag created on preferred datastore
func deleteTagCreatedForPreferredDatastore(masterIp string, tagName []string) error {
	for i := 0; i < len(tagName); i++ {
		deleteTagCat := govcLoginCmd() +
			"govc tags.rm -f -c " + preferredDSCat + " " + tagName[i]
		framework.Logf("Deleting tag created for preferred datastore: %s ", deleteTagCat)
		deleteTagCatRes, err := sshExec(sshClientConfig, masterIp, deleteTagCat)
		if err != nil && deleteTagCatRes.Code != 0 {
			fssh.LogResult(deleteTagCatRes)
			return fmt.Errorf("couldn't execute command: %s on host: %v , error: %s",
				deleteTagCat, masterIp, err)
		}
	}
	return nil
}

// createTagForPreferredDatastore method is used to create tag required for choosing preferred datastore
func createTagForPreferredDatastore(masterIp string, tagName []string) error {
	for i := 0; i < len(tagName); i++ {
		createTagCat := govcLoginCmd() +
			"govc tags.create -d '" + preferredTagDesc + "' -c " + preferredDSCat + " " + tagName[i]
		framework.Logf("Creating tag for preferred datastore: %s ", createTagCat)
		createTagCatRes, err := sshExec(sshClientConfig, masterIp, createTagCat)
		if err != nil && createTagCatRes.Code != 0 {
			fssh.LogResult(createTagCatRes)
			return fmt.Errorf("couldn't execute command: %s on host: %v , error: %s",
				createTagCat, masterIp, err)
		}
	}
	return nil
}
