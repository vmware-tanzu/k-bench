/*
Copyright 2019-2020 VMware, Inc.

SPDX-License-Identifier: Apache-2.0

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

package manager

import (
	"sort"
	"strconv"
	"context"
	//"strings"
	"fmt"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"k-bench/perf_util"
	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
)

const deploymentNamePrefix string = "kbench-deployment-"

/*
 * DeploymentManager manages deployment actions and stats.
 */
type DeploymentManager struct {
	// This is a shared client
	client *kubernetes.Clientset
	// This is an array of clients used for deployment operations
	clientsets []*kubernetes.Clientset
	// A map to track the API response time for the supported actions
	apiTimes map[string][]time.Duration

	namespace string
	source    string

	depNs map[string]string // Used to track deployments to namespaces mappings
	nsSet map[string]bool   // Used to track created non-default namespaces

	podMgr *PodManager

	// Mutex to update api latency
	alMutex sync.Mutex
	// Mutex to update deployment set
	depMutex sync.Mutex

	// Action functions
	ActionFuncs map[string]func(*DeploymentManager, interface{}) error
}

func NewDeploymentManager() Manager {
	apt := make(map[string][]time.Duration, 0)

	dn := make(map[string]string, 0)
	ns := make(map[string]bool, 0)

	af := make(map[string]func(*DeploymentManager, interface{}) error, 0)

	af[CREATE_ACTION] = (*DeploymentManager).Create
	af[DELETE_ACTION] = (*DeploymentManager).Delete
	af[LIST_ACTION] = (*DeploymentManager).List
	af[GET_ACTION] = (*DeploymentManager).Get
	af[UPDATE_ACTION] = (*DeploymentManager).Update
	af[SCALE_ACTION] = (*DeploymentManager).Scale

	return &DeploymentManager{
		apiTimes: apt,

		namespace: apiv1.NamespaceDefault,
		depNs:     dn,
		nsSet:     ns,

		alMutex:  sync.Mutex{},
		depMutex: sync.Mutex{},

		podMgr: nil,

		ActionFuncs: af,
	}
}

/*
 * This function is used to initialize the manager.
 */
func (mgr *DeploymentManager) Init(
	kubeConfig *restclient.Config,
	nsName string,
	maxClients int,
	resourceType string,
) {
	mgr.namespace = nsName
	mgr.source = perf_util.GetHostnameFromUrl(kubeConfig.Host)

	podmgr, _ := GetManager("Pod")

	pm := podmgr.(*PodManager)

	mgr.podMgr = pm

	sharedClient, err := kubernetes.NewForConfig(kubeConfig)

	if err != nil {
		panic(err)
	}

	mgr.client = sharedClient

	mgr.clientsets = make([]*kubernetes.Clientset, maxClients)

	for i := 0; i < maxClients; i++ {
		client, ce := kubernetes.NewForConfig(kubeConfig)

		if ce != nil {
			panic(ce)
		}

		mgr.clientsets[i] = client
	}

	nsSpec := &apiv1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	_, cerr := mgr.client.CoreV1().Namespaces().Create(context.Background(), nsSpec, metav1.CreateOptions{})
	if cerr != nil {
		log.Warningf("Fail to create namespace %s, %v", nsName, err)
	} else {
		mgr.nsSet[nsName] = true
	}
	mgr.podMgr.Init(kubeConfig, nsName, false, 0, resourceType)
}

/*
 * This function implements the CREATE action.
 */
func (mgr *DeploymentManager) Create(spec interface{}) error {

	switch s := spec.(type) {
	default:
		log.Errorf("Invalid spec type %T for Deployment create action.", s)
		return fmt.Errorf("Invalid spec type %T for Deployment create action.", s)
	case *appsv1.Deployment:
		tid, _ := strconv.Atoi(s.Labels["tid"])
		cid := tid % len(mgr.clientsets)

		ns := mgr.namespace
		if s.Namespace != "" {
			ns = s.Namespace
			mgr.depMutex.Lock()
			if _, exist := mgr.nsSet[ns]; !exist && ns != apiv1.NamespaceDefault {
				nsSpec := &apiv1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
				_, err := mgr.client.CoreV1().Namespaces().Create(context.Background(), nsSpec, metav1.CreateOptions{})
				mgr.nsSet[ns] = true
				if err != nil {
					if strings.Contains(err.Error(), "already exists") {
						mgr.nsSet[ns] = true
					} else {
						log.Warningf("Fail to create namespace %s, %v", ns, err)
					}
				} else {
					mgr.nsSet[ns] = true
				}
			}
			mgr.depMutex.Unlock()
		}

		startTime := metav1.Now()
		dep, err := mgr.clientsets[cid].AppsV1().Deployments(ns).Create(context.Background(), s, metav1.CreateOptions{})

		latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

		if err != nil {
			return err
		}

		mgr.alMutex.Lock()
		mgr.apiTimes[CREATE_ACTION] = append(mgr.apiTimes[CREATE_ACTION], latency)
		mgr.alMutex.Unlock()

		mgr.depMutex.Lock()
		mgr.depNs[dep.Name] = ns
		mgr.depMutex.Unlock()
	}
	return nil
}

/*
 * This function implements the LIST action.
 */
func (mgr *DeploymentManager) List(n interface{}) error {

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec type %T for Deployment list action.", s)
		return fmt.Errorf("Invalid spec type %T for Deployment list action.", s)
	case ActionSpec:
		options := GetListOptions(s)

		cid := s.Tid % len(mgr.clientsets)

		ns := mgr.namespace
		if s.Namespace != "" {
			ns = s.Namespace
		}

		startTime := metav1.Now()
		deps, err := mgr.clientsets[cid].AppsV1().Deployments(ns).List(context.Background(), options)
		latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

		if err != nil {
			return err
		}
		log.Infof("Listed %v deployments", len(deps.Items))

		mgr.alMutex.Lock()
		mgr.apiTimes[LIST_ACTION] = append(mgr.apiTimes[LIST_ACTION], latency)
		mgr.alMutex.Unlock()
	}
	return nil
}

/*
 * This function implements the GET action.
 */
func (mgr *DeploymentManager) Get(n interface{}) error {

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec type %T for Deployment get action.", s)
		return fmt.Errorf("Invalid spec type %T for Deployment get action.", s)
	case ActionSpec:
		cid := s.Tid % len(mgr.clientsets)

		ns := mgr.namespace
		if s.Namespace != "" {
			ns = s.Namespace
		}

		startTime := metav1.Now()
		dep, err := mgr.clientsets[cid].AppsV1().Deployments(ns).
			Get(context.Background(), s.Name, metav1.GetOptions{})
		latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

		if err != nil {
			return err
		}

		log.Infof("Got deployment %v", dep.Name)

		mgr.alMutex.Lock()
		mgr.apiTimes[GET_ACTION] = append(mgr.apiTimes[GET_ACTION], latency)
		mgr.alMutex.Unlock()
	}
	return nil
}

/*
 * This function implements the UPDATE action.
 */
func (mgr *DeploymentManager) Update(n interface{}) error {

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec type %T for Deployment update action.", s)
		return fmt.Errorf("Invalid spec type %T for Deployment update action.", s)
	case ActionSpec:
		options := GetListOptions(s)

		cid := s.Tid % len(mgr.clientsets)

		ns := mgr.namespace
		if s.Namespace != "" {
			ns = s.Namespace
		}

		deps := make([]appsv1.Deployment, 0)

		depList, err := mgr.clientsets[cid].AppsV1().Deployments(ns).List(context.Background(), options)
		if err != nil {
			return err
		}
		deps = depList.Items

		for _, currDep := range deps {
			newPdls := int32(100000)
			currDep.Spec.ProgressDeadlineSeconds = &newPdls

			startTime := metav1.Now()
			dep, err := mgr.clientsets[cid].AppsV1().Deployments(ns).Update(context.Background(), &currDep, metav1.UpdateOptions{})
			latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

			if err != nil {
				return err
			}
			log.Infof("Updated ProgressDeadlineSeconds for deployments %v", dep.Name)

			mgr.alMutex.Lock()
			mgr.apiTimes[UPDATE_ACTION] = append(mgr.apiTimes[UPDATE_ACTION], latency)
			mgr.alMutex.Unlock()
		}
	}
	return nil
}

/*
 * This function implements the SCALE action.
 */
func (mgr *DeploymentManager) Scale(n interface{}) error {

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec type %T for Deployment scale action.", s)
		return fmt.Errorf("Invalid spec type %T for Deployment scale action.", s)
	case ActionSpec:
		options := GetListOptions(s)

		cid := s.Tid % len(mgr.clientsets)

		ns := mgr.namespace
		if s.Namespace != "" {
			ns = s.Namespace
		}

		deps := make([]appsv1.Deployment, 0)

		depList, err := mgr.clientsets[cid].AppsV1().Deployments(ns).List(context.Background(), options)
		if err != nil {
			return err
		}
		deps = depList.Items

		for _, currDep := range deps {
			scale, ge := mgr.clientsets[cid].AppsV1().Deployments(ns).GetScale(
				context.Background(), currDep.Name, metav1.GetOptions{})
			if ge != nil {
				return ge
			}

			scale.Spec.Replicas += 1

			startTime := metav1.Now()
			_, err := mgr.clientsets[cid].AppsV1().Deployments(ns).UpdateScale(
				context.Background(), currDep.Name, scale, metav1.UpdateOptions{})
			latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

			if err != nil {
				return err
			}
			log.Infof("Updated scale to %v for deployments %v", scale.Spec.Replicas,
				currDep.Name)

			mgr.alMutex.Lock()
			mgr.apiTimes[SCALE_ACTION] = append(mgr.apiTimes[SCALE_ACTION], latency)
			mgr.alMutex.Unlock()
		}
	}
	return nil
}

/*
 * This function implements the DELETE action.
 */
func (mgr *DeploymentManager) Delete(n interface{}) error {
	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec %T for Deployment delete action.", s)
		return fmt.Errorf("Invalid spec %T for Deployment delete action.", s)
	case ActionSpec:
		cid := s.Tid % len(mgr.clientsets)

		ns := mgr.namespace
		if s.Namespace != "" {
			ns = s.Namespace
		}

		selector := fields.Set{
			"metadata.namespace": ns,
		}.AsSelector().String()
		podOptions := metav1.ListOptions{FieldSelector: selector}
		pods, err := mgr.clientsets[cid].CoreV1().Pods(ns).List(context.Background(), podOptions)

		if err != nil {
			return err
		}

		podName := ""
		for _, pod := range pods.Items {
			if _, ok := mgr.podMgr.scheduleTimes[pod.Name]; !ok {
				podName = pod.Name
				break
			}
		}

		if podName != "" {
			mgr.podMgr.UpdateBeforeDeletion(podName, ns)
		}

		deps := make([]appsv1.Deployment, 0)

		options := GetListOptions(s)
		depList, err := mgr.clientsets[cid].AppsV1().Deployments(ns).List(context.Background(), options)
		if err != nil {
			return err
		}
		deps = depList.Items

		for _, currDep := range deps {
			// Delete the deployment
			log.Infof("Deleting deployment %v", currDep.Name)
			startTime := metav1.Now()
			mgr.clientsets[cid].AppsV1().Deployments(ns).Delete(context.Background(), currDep.Name, metav1.DeleteOptions{})

			latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

			mgr.alMutex.Lock()
			mgr.apiTimes[DELETE_ACTION] = append(mgr.apiTimes[DELETE_ACTION], latency)
			mgr.alMutex.Unlock()

			mgr.depMutex.Lock()
			// Delete it from the deployment set
			_, ok := mgr.depNs[currDep.Name]

			if ok {
				delete(mgr.depNs, currDep.Name)
			}
			mgr.depMutex.Unlock()
		}
	}
	return nil
}

/*
 * This function implements the DeleteAll manager interface. It is used to clean
 * all the resources that are created by the deployment manager.
 */
func (mgr *DeploymentManager) DeleteAll() error {

	if len(mgr.depNs) > 0 {
		log.Infof("Deleting all deployments created by the deployment manager...")
		for name, _ := range mgr.depNs {
			// Just use tid 0 so that the first client is used to delete all deployments
			mgr.Delete(ActionSpec{
				Name:      name,
				Tid:       0,
				Namespace: mgr.depNs[name]})
		}
		mgr.depNs = make(map[string]string, 0)
	} else {
		log.Infof("Found no deployment to delete, maybe they have already been deleted.")
	}

	if mgr.namespace != apiv1.NamespaceDefault {
		mgr.client.CoreV1().Namespaces().Delete(context.Background(), mgr.namespace, metav1.DeleteOptions{})
	}

	// Delete other non default namespaces
	for ns, _ := range mgr.nsSet {
		if ns != apiv1.NamespaceDefault {
			mgr.client.CoreV1().Namespaces().Delete(context.Background(), ns, metav1.DeleteOptions{})
		}
	}
	mgr.nsSet = make(map[string]bool, 0)

	close(mgr.podMgr.podChan)

	return nil
}

/*
 * This function returns whether all the pods in the deployment are ready
 */
func (mgr *DeploymentManager) IsStable() bool {
	return len(mgr.podMgr.cReadyTimes) != 0 &&
		len(mgr.podMgr.cReadyTimes) == len(mgr.podMgr.cFirstTimes)
}

/*
 * This function computes all the metrics and stores the results into the log file.
 */
func (mgr *DeploymentManager) LogStats() {
	mgr.podMgr.LogStats()

	log.Infof("----------------------------- Deployment API Call Latencies (ms) " +
		"-----------------------------")
	log.Infof("%-50v %-10v %-10v %-10v %-10v", " ", "median", "min", "max", "99%")

	for m, _ := range mgr.apiTimes {
		sort.Slice(mgr.apiTimes[m],
			func(i, j int) bool { return mgr.apiTimes[m][i] < mgr.apiTimes[m][j] })
		mid := float32(mgr.apiTimes[m][len(mgr.apiTimes[m])/2]) / float32(time.Millisecond)
		min := float32(mgr.apiTimes[m][0]) / float32(time.Millisecond)
		max := float32(mgr.apiTimes[m][len(mgr.apiTimes[m])-1]) / float32(time.Millisecond)
		p99 := float32(mgr.apiTimes[m][len(mgr.apiTimes[m])-1-len(mgr.apiTimes[m])/100]) /
			float32(time.Millisecond)
		log.Infof("%-50v %-10v %-10v %-10v %-10v", m+" deployment latency: ",
			mid, min, max, p99)
	}
}

func (mgr *DeploymentManager) GetResourceName(opNum int, tid int) string {
	return deploymentNamePrefix + "oid-" + strconv.Itoa(opNum) + "-tid-" + strconv.Itoa(tid)
}

func (mgr *DeploymentManager) SendMetricToWavefront(now time.Time, wfTags []perf_util.WavefrontTag, wavefrontPathDir string, prefix string) {
	mgr.podMgr.SendMetricToWavefront(now, wfTags, wavefrontPathDir, "deployment.")
	var points []perf_util.WavefrontDataPoint
	for m, _ := range mgr.apiTimes {
		mid := float32(mgr.apiTimes[m][len(mgr.apiTimes[m])/2]) / float32(time.Millisecond)
		min := float32(mgr.apiTimes[m][0]) / float32(time.Millisecond)
		max := float32(mgr.apiTimes[m][len(mgr.apiTimes[m])-1]) / float32(time.Millisecond)
		p99 := float32(mgr.apiTimes[m][len(mgr.apiTimes[m])-1-len(mgr.apiTimes[m])/100]) /
			float32(time.Millisecond)
		points = append(points, perf_util.WavefrontDataPoint{"deployment.apicall." + m + ".median.latency",
			mid, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"deployment.apicall." + m + ".min.latency",
			min, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"deployment.apicall." + m + ".max.latency",
			max, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"deployment.apicall." + m + ".p99.latency",
			p99, now, mgr.source, wfTags})

	}
	var metricLines []string
	for _, point := range points {
		metricLines = append(metricLines, point.String())
		metricLines = append(metricLines, "\n")
	}

	perf_util.WriteDataPoints(now, points, wavefrontPathDir, prefix)
}

func (mgr *DeploymentManager) CalculateStats() {
	mgr.podMgr.CalculateStats()
}

func (mgr *DeploymentManager) CalculateSuccessRate() int {
	return mgr.podMgr.CalculateSuccessRate()
}
