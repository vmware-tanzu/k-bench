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
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"
	"context"

	log "github.com/sirupsen/logrus"
	//appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	restclient "k8s.io/client-go/rest"
	//"k8s.io/apimachinery/pkg/fields"
	"k-bench/perf_util"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

const namespaceNamePrefix string = "kbench-namespace-"

/*
 * NamespaceManager manages namespace actions and stats.
 */
type NamespaceManager struct {
	// This is a shared client
	client *kubernetes.Clientset
	// This is an array of clients used for namespace operations
	clientsets []*kubernetes.Clientset
	// A map to track the API response time for the supported actions
	apiTimes map[string][]time.Duration
	source   string

	// Mutex to update api latency
	alMutex sync.Mutex

	// Action functions
	ActionFuncs map[string]func(*NamespaceManager, interface{}) error
}

func NewNamespaceManager() Manager {
	apt := make(map[string][]time.Duration, 0)

	af := make(map[string]func(*NamespaceManager, interface{}) error, 0)

	af[CREATE_ACTION] = (*NamespaceManager).Create
	af[DELETE_ACTION] = (*NamespaceManager).Delete
	af[LIST_ACTION] = (*NamespaceManager).List
	af[GET_ACTION] = (*NamespaceManager).Get
	af[UPDATE_ACTION] = (*NamespaceManager).Update

	return &NamespaceManager{
		apiTimes: apt,

		alMutex: sync.Mutex{},

		ActionFuncs: af,
	}
}

/*
 * This function implements the Init interface and is used to initialize the manager.
 */
func (mgr *NamespaceManager) Init(
	kubeConfig *restclient.Config,
	nsName string,
	maxClients int,
) {

	mgr.source = perf_util.GetHostnameFromUrl(kubeConfig.Host)
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
}

/*
 * This function implements the CREATE action.
 */
func (mgr *NamespaceManager) Create(spec interface{}) error {

	switch s := spec.(type) {
	default:
		log.Errorf("Invalid spec type %T for Namespace create action.", s)
		return fmt.Errorf("Invalid spec type %T for Namespace create action.", s)
	case *apiv1.Namespace:
		tid, _ := strconv.Atoi(s.Labels["tid"])
		cid := tid % len(mgr.clientsets)
		startTime := metav1.Now()
		_, err := mgr.clientsets[cid].CoreV1().Namespaces().Create(context.Background(), s, metav1.CreateOptions{})

		latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

		if err != nil {
			return err
		}

		mgr.alMutex.Lock()
		mgr.apiTimes[CREATE_ACTION] = append(mgr.apiTimes[CREATE_ACTION], latency)
		mgr.alMutex.Unlock()
	}
	return nil
}

/*
 * This function implements the LIST action.
 */
func (mgr *NamespaceManager) List(n interface{}) error {

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec type %T for Namespace list action.", s)
		return fmt.Errorf("Invalid spec type %T for Namespace list action.", s)
	case ActionSpec:

		options := GetListOptions(s)

		cid := s.Tid % len(mgr.clientsets)

		startTime := metav1.Now()
		nss, err := mgr.clientsets[cid].CoreV1().Namespaces().List(context.Background(), options)
		latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

		if err != nil {
			return err
		}
		log.Infof("Listed %v namespaces", len(nss.Items))

		mgr.alMutex.Lock()
		mgr.apiTimes[LIST_ACTION] = append(mgr.apiTimes[LIST_ACTION], latency)
		mgr.alMutex.Unlock()
	}
	return nil
}

/*
 * This function implements the GET action.
 */
func (mgr *NamespaceManager) Get(n interface{}) error {

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec type %T for Namespace get action.", s)
		return fmt.Errorf("Invalid spec type %T for Namespace get action.", s)
	case ActionSpec:
		cid := s.Tid % len(mgr.clientsets)
		startTime := metav1.Now()
		ns, err := mgr.clientsets[cid].CoreV1().Namespaces().Get(
			context.Background(), s.Name, metav1.GetOptions{})
		latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

		if err != nil {
			return err
		}

		log.Infof("Got namespace %v", ns.Name)

		mgr.alMutex.Lock()
		mgr.apiTimes[GET_ACTION] = append(mgr.apiTimes[GET_ACTION], latency)
		mgr.alMutex.Unlock()
	}
	return nil
}

/*
 * This function implements the UPDATE action.
 */
func (mgr *NamespaceManager) Update(n interface{}) error {

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec type %T for Namespace update action.", s)
		return fmt.Errorf("Invalid spec type %T for Namespace update action.", s)
	case ActionSpec:
		options := GetListOptions(s)

		cid := s.Tid % len(mgr.clientsets)
		nsList, err := mgr.clientsets[cid].CoreV1().Namespaces().List(context.Background(), options)
		if err != nil {
			return err
		}

		nss := nsList.Items

		for _, currNs := range nss {
			startTime := metav1.Now()
			ns, ue := mgr.clientsets[cid].CoreV1().Namespaces().Update(context.Background(), &currNs, metav1.UpdateOptions{})
			latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

			if ue != nil {
				return ue
			}
			log.Infof("Updated namespace %v", ns.Name)

			mgr.alMutex.Lock()
			mgr.apiTimes[UPDATE_ACTION] = append(mgr.apiTimes[UPDATE_ACTION], latency)
			mgr.alMutex.Unlock()
		}
	}
	return nil
}

/*
 * This function implements the DELETE action.
 */
func (mgr *NamespaceManager) Delete(n interface{}) error {
	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec %T for Namespace delete action.", s)
		return fmt.Errorf("Invalid spec %T for Namespace delete action.", s)
	case ActionSpec:
		cid := s.Tid % len(mgr.clientsets)

		options := GetListOptions(s)
		nsList, err := mgr.clientsets[cid].CoreV1().Namespaces().List(context.Background(), options)
		if err != nil {
			return err
		}

		nss := nsList.Items

		for _, currNs := range nss {
			log.Infof("Deleting namespace %v", currNs.Name)
			// Delete the namespace
			startTime := metav1.Now()
			mgr.clientsets[cid].CoreV1().Namespaces().Delete(context.Background(), currNs.Name, metav1.DeleteOptions{})

			latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

			mgr.alMutex.Lock()
			mgr.apiTimes[DELETE_ACTION] = append(mgr.apiTimes[DELETE_ACTION], latency)
			mgr.alMutex.Unlock()
		}
	}
	return nil
}

/*
 * This function implements the DeleteAll manager interface. It is used to clean
 * all the resources that are created by the namespace manager.
 */
func (mgr *NamespaceManager) DeleteAll() error {

	options := metav1.ListOptions{LabelSelector: labels.SelectorFromSet(
		labels.Set{"app": AppName}).String()}
	nss, err := mgr.client.CoreV1().Namespaces().List(context.Background(), options)

	if err != nil {
		return err
	}

	if len(nss.Items) > 0 {
		log.Infof("Deleting %v namespaces created by the manager...", len(nss.Items))
		for _, ns := range nss.Items {
			mgr.Delete(ActionSpec{
				Name: ns.Name,
				Tid:  0})
		}
	} else {
		log.Infof("Found no namespaces to delete, maybe they have already been deleted.")
	}

	return nil
}

/*
 * This returns the difference between the number of running pods and created pods
 *
func (mgr *NamespaceManager) IsStable() bool {
	return len(mgr.podMgr.cReadyTimes) != 0 &&
		len(mgr.podMgr.cReadyTimes) == len(mgr.podMgr.cFirstTimes)
}*/

/*
 * This function computes all the metrics and stores the results into the log file.
 */
func (mgr *NamespaceManager) LogStats() {

	log.Infof("----------------------------- Namespace API Call Latencies (ms) " +
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
		log.Infof("%-50v %-10v %-10v %-10v %-10v", m+" namespace latency: ",
			mid, min, max, p99)
	}
}

func (mgr *NamespaceManager) GetResourceName(opNum int, tid int) string {
	return namespaceNamePrefix + "oid-" + strconv.Itoa(opNum) + "-tid-" + strconv.Itoa(tid)
}

func (mgr *NamespaceManager) SendMetricToWavefront(now time.Time, wfTags []perf_util.WavefrontTag, wavefrontPathDir string, prefix string) {
	var points []perf_util.WavefrontDataPoint
	for m, _ := range mgr.apiTimes {
		mid := float32(mgr.apiTimes[m][len(mgr.apiTimes[m])/2]) / float32(time.Millisecond)
		min := float32(mgr.apiTimes[m][0]) / float32(time.Millisecond)
		max := float32(mgr.apiTimes[m][len(mgr.apiTimes[m])-1]) / float32(time.Millisecond)
		p99 := float32(mgr.apiTimes[m][len(mgr.apiTimes[m])-1-len(mgr.apiTimes[m])/100]) /
			float32(time.Millisecond)
		points = append(points, perf_util.WavefrontDataPoint{"namespace.apicall." + m + ".median.latency",
			mid, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"namespace.apicall." + m + ".min.latency",
			min, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"namespace.apicall." + m + ".max.latency",
			max, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"namespace.apicall." + m + ".p99.latency",
			p99, now, mgr.source, wfTags})

	}
	var metricLines []string
	for _, point := range points {
		metricLines = append(metricLines, point.String())
		metricLines = append(metricLines, "\n")
	}

	perf_util.WriteDataPoints(now, points, wavefrontPathDir, prefix)
}

func (mgr *NamespaceManager) CalculateStats() {
	// This manager has nothing to calculate
}

func (mgr *NamespaceManager) CalculateSuccessRate() int {
	// This manager has nothing to calculate
	return 0
}
