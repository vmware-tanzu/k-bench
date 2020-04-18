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

const statefulsetNamePrefix string = "kbench-statefulset-"

/*
 * StatefulSetManager manages statefulset actions and stats.
 * TODO: factor this out with DeploymentManager
 */
type StatefulSetManager struct {
	// This is a shared client
	client *kubernetes.Clientset
	// This is an array of clients used for statefulset operations
	clientsets []*kubernetes.Clientset
	// A map to track the API response time for the supported actions
	apiTimes map[string][]time.Duration

	namespace string
	source    string

	ssNs  map[string]string // Used to track statefulset to namespaces mappings
	nsSet map[string]bool   // Used to track created non-default namespaces

	podMgr *PodManager

	// Mutex to update api latency
	alMutex sync.Mutex
	// Mutex to update statefulset set
	ssMutex sync.Mutex

	// Action functions
	ActionFuncs map[string]func(*StatefulSetManager, interface{}) error
}

func NewStatefulSetManager() Manager {
	apt := make(map[string][]time.Duration, 0)

	dn := make(map[string]string, 0)
	ns := make(map[string]bool, 0)

	af := make(map[string]func(*StatefulSetManager, interface{}) error, 0)

	af[CREATE_ACTION] = (*StatefulSetManager).Create
	af[DELETE_ACTION] = (*StatefulSetManager).Delete
	af[LIST_ACTION] = (*StatefulSetManager).List
	af[GET_ACTION] = (*StatefulSetManager).Get
	af[UPDATE_ACTION] = (*StatefulSetManager).Update
	af[SCALE_ACTION] = (*StatefulSetManager).Scale

	return &StatefulSetManager{
		apiTimes: apt,

		namespace: apiv1.NamespaceDefault,
		ssNs:      dn,
		nsSet:     ns,

		alMutex: sync.Mutex{},
		ssMutex: sync.Mutex{},

		podMgr: nil,

		ActionFuncs: af,
	}
}

/*
 * This function is used to initialize the manager.
 */
func (mgr *StatefulSetManager) Init(
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
	_, cerr := mgr.client.CoreV1().Namespaces().Create(nsSpec)
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
func (mgr *StatefulSetManager) Create(spec interface{}) error {

	switch s := spec.(type) {
	default:
		log.Errorf("Invalid spec type %T for StatefulSet create action.", s)
		return fmt.Errorf("Invalid spec type %T for StatefulSet create action.", s)
	case *appsv1.StatefulSet:
		tid, _ := strconv.Atoi(s.Labels["tid"])
		cid := tid % len(mgr.clientsets)

		ns := mgr.namespace
		if s.Namespace != "" {
			ns = s.Namespace
			mgr.ssMutex.Lock()
			if _, exist := mgr.nsSet[ns]; !exist && ns != apiv1.NamespaceDefault {
				nsSpec := &apiv1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
				_, err := mgr.client.CoreV1().Namespaces().Create(nsSpec)
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
			mgr.ssMutex.Unlock()
		}

		startTime := metav1.Now()
		ss, err := mgr.clientsets[cid].AppsV1().StatefulSets(ns).Create(s)

		latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

		if err != nil {
			return err
		}

		mgr.alMutex.Lock()
		mgr.apiTimes[CREATE_ACTION] = append(mgr.apiTimes[CREATE_ACTION], latency)
		mgr.alMutex.Unlock()

		mgr.ssMutex.Lock()
		mgr.ssNs[ss.Name] = ns
		mgr.ssMutex.Unlock()
	}
	return nil
}

/*
 * This function implements the LIST action.
 */
func (mgr *StatefulSetManager) List(n interface{}) error {

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec type %T for StatefulSet list action.", s)
		return fmt.Errorf("Invalid spec type %T for StatefuleSet list action.", s)
	case ActionSpec:
		options := GetListOptions(s)

		cid := s.Tid % len(mgr.clientsets)

		ns := mgr.namespace
		if s.Namespace != "" {
			ns = s.Namespace
		}

		startTime := metav1.Now()
		sss, err := mgr.clientsets[cid].AppsV1().StatefulSets(ns).List(options)
		latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

		if err != nil {
			return err
		}
		log.Infof("Listed %v statefulsets", len(sss.Items))

		mgr.alMutex.Lock()
		mgr.apiTimes[LIST_ACTION] = append(mgr.apiTimes[LIST_ACTION], latency)
		mgr.alMutex.Unlock()
	}
	return nil
}

/*
 * This function implements the GET action.
 */
func (mgr *StatefulSetManager) Get(n interface{}) error {

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec type %T for StatefulSet get action.", s)
		return fmt.Errorf("Invalid spec type %T for StatefulSet get action.", s)
	case ActionSpec:
		cid := s.Tid % len(mgr.clientsets)

		ns := mgr.namespace
		if s.Namespace != "" {
			ns = s.Namespace
		}

		startTime := metav1.Now()
		ss, err := mgr.clientsets[cid].AppsV1().StatefulSets(ns).
			Get(s.Name, metav1.GetOptions{})
		latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

		if err != nil {
			return err
		}

		log.Infof("Got statefulset %v", ss.Name)

		mgr.alMutex.Lock()
		mgr.apiTimes[GET_ACTION] = append(mgr.apiTimes[GET_ACTION], latency)
		mgr.alMutex.Unlock()
	}
	return nil
}

/*
 * This function implements the UPDATE action.
 */
func (mgr *StatefulSetManager) Update(n interface{}) error {

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec type %T for StatefulSet update action.", s)
		return fmt.Errorf("Invalid spec type %T for StatefulSet update action.", s)
	case ActionSpec:
		options := GetListOptions(s)

		cid := s.Tid % len(mgr.clientsets)

		ns := mgr.namespace
		if s.Namespace != "" {
			ns = s.Namespace
		}

		sss := make([]appsv1.StatefulSet, 0)

		ssList, err := mgr.clientsets[cid].AppsV1().StatefulSets(ns).List(options)
		if err != nil {
			return err
		}
		sss = ssList.Items

		for _, currSs := range sss {
			newRhl := int32(10)
			currSs.Spec.RevisionHistoryLimit = &newRhl

			startTime := metav1.Now()
			ss, err := mgr.clientsets[cid].AppsV1().StatefulSets(ns).Update(&currSs)
			latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

			if err != nil {
				return err
			}
			log.Infof("Updated RevisionHistoryLimit for statefulset %v", ss.Name)

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
func (mgr *StatefulSetManager) Scale(n interface{}) error {

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec type %T for StatefulSet scale action.", s)
		return fmt.Errorf("Invalid spec type %T for StatefulSet scale action.", s)
	case ActionSpec:
		options := GetListOptions(s)

		cid := s.Tid % len(mgr.clientsets)

		ns := mgr.namespace
		if s.Namespace != "" {
			ns = s.Namespace
		}

		sss := make([]appsv1.StatefulSet, 0)

		ssList, err := mgr.clientsets[cid].AppsV1().StatefulSets(ns).List(options)
		if err != nil {
			return err
		}
		sss = ssList.Items

		for _, currSs := range sss {
			scale, ge := mgr.clientsets[cid].AppsV1().StatefulSets(ns).GetScale(
				currSs.Name, metav1.GetOptions{})
			if ge != nil {
				return ge
			}

			scale.Spec.Replicas += 1

			startTime := metav1.Now()
			_, err := mgr.clientsets[cid].AppsV1().StatefulSets(ns).UpdateScale(
				currSs.Name, scale)
			latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

			if err != nil {
				return err
			}
			log.Infof("Updated scale to %v for statefulsets %v", scale.Spec.Replicas,
				currSs.Name)

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
func (mgr *StatefulSetManager) Delete(n interface{}) error {
	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec %T for StatefulSet delete action.", s)
		return fmt.Errorf("Invalid spec %T for StatefulSet delete action.", s)
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
		pods, err := mgr.clientsets[cid].CoreV1().Pods(ns).List(podOptions)

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

		sss := make([]appsv1.StatefulSet, 0)

		options := GetListOptions(s)
		ssList, err := mgr.clientsets[cid].AppsV1().StatefulSets(ns).List(options)
		if err != nil {
			return err
		}
		sss = ssList.Items

		for _, currSs := range sss {
			// Delete the statefulset
			log.Infof("Deleting statefulset %v", currSs.Name)
			startTime := metav1.Now()
			mgr.clientsets[cid].AppsV1().StatefulSets(ns).Delete(currSs.Name, nil)

			latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

			mgr.alMutex.Lock()
			mgr.apiTimes[DELETE_ACTION] = append(mgr.apiTimes[DELETE_ACTION], latency)
			mgr.alMutex.Unlock()

			mgr.ssMutex.Lock()
			// Delete it from the statefulset set
			_, ok := mgr.ssNs[currSs.Name]

			if ok {
				delete(mgr.ssNs, currSs.Name)
			}
			mgr.ssMutex.Unlock()
		}
	}
	return nil
}

/*
 * This function implements the DeleteAll manager interface. It is used to clean
 * all the resources that are created by the statefulset manager.
 */
func (mgr *StatefulSetManager) DeleteAll() error {

	if len(mgr.ssNs) > 0 {
		log.Infof("Deleting all stateful sets created by the StatefulSet manager...")
		for name, _ := range mgr.ssNs {
			// Just use tid 0 so that the first client is used to delete all statefulsets
			mgr.Delete(ActionSpec{
				Name:      name,
				Tid:       0,
				Namespace: mgr.ssNs[name]})
		}
		mgr.ssNs = make(map[string]string, 0)
	} else {
		log.Infof("Found no statefulset to delete, maybe they have already been deleted.")
	}

	if mgr.namespace != apiv1.NamespaceDefault {
		mgr.client.CoreV1().Namespaces().Delete(mgr.namespace, nil)
	}

	// Delete other non default namespaces
	for ns, _ := range mgr.nsSet {
		if ns != apiv1.NamespaceDefault {
			mgr.client.CoreV1().Namespaces().Delete(ns, nil)
		}
	}
	mgr.nsSet = make(map[string]bool, 0)

	close(mgr.podMgr.podChan)

	return nil
}

/*
 * This function returns whether all the pods in the statefulset are ready
 */
func (mgr *StatefulSetManager) IsStable() bool {
	return len(mgr.podMgr.cReadyTimes) != 0 &&
		len(mgr.podMgr.cReadyTimes) == len(mgr.podMgr.cFirstTimes)
}

/*
 * This function computes all the metrics and stores the results into the log file.
 */
func (mgr *StatefulSetManager) LogStats() {
	mgr.podMgr.LogStats()

	log.Infof("----------------------------- StatefulSet API Call Latencies (ms) " +
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
		log.Infof("%-50v %-10v %-10v %-10v %-10v", m+" statefulset latency: ",
			mid, min, max, p99)
	}
}

func (mgr *StatefulSetManager) GetResourceName(opNum int, tid int) string {
	return statefulsetNamePrefix + "oid-" + strconv.Itoa(opNum) + "-tid-" + strconv.Itoa(tid)
}

func (mgr *StatefulSetManager) SendMetricToWavefront(now time.Time, wfTags []perf_util.WavefrontTag, wavefrontPathDir string, prefix string) {
	mgr.podMgr.SendMetricToWavefront(now, wfTags, wavefrontPathDir, "statefulset.")
	var points []perf_util.WavefrontDataPoint
	for m, _ := range mgr.apiTimes {
		mid := float32(mgr.apiTimes[m][len(mgr.apiTimes[m])/2]) / float32(time.Millisecond)
		min := float32(mgr.apiTimes[m][0]) / float32(time.Millisecond)
		max := float32(mgr.apiTimes[m][len(mgr.apiTimes[m])-1]) / float32(time.Millisecond)
		p99 := float32(mgr.apiTimes[m][len(mgr.apiTimes[m])-1-len(mgr.apiTimes[m])/100]) /
			float32(time.Millisecond)
		points = append(points, perf_util.WavefrontDataPoint{"statefulset.apicall." + m + ".median.latency",
			mid, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"statefulset.apicall." + m + ".min.latency",
			min, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"statefulset.apicall." + m + ".max.latency",
			max, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"statefulset.apicall." + m + ".p99.latency",
			p99, now, mgr.source, wfTags})

	}
	var metricLines []string
	for _, point := range points {
		metricLines = append(metricLines, point.String())
		metricLines = append(metricLines, "\n")
	}

	perf_util.WriteDataPoints(now, points, wavefrontPathDir, prefix)
}

func (mgr *StatefulSetManager) CalculateStats() {
	mgr.podMgr.CalculateStats()
}

func (mgr *StatefulSetManager) CalculateSuccessRate() int {
	return mgr.podMgr.CalculateSuccessRate()
}
