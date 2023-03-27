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
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

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

const serviceNamePrefix string = "kbench-service-"

/*
 * ServiceManager manages service actions and stats.
 */
type ServiceManager struct {
	// This is a shared client
	client *kubernetes.Clientset
	// This is an array of clients used for service operations
	clientsets []*kubernetes.Clientset
	// A map to track the API response time for the supported actions
	apiTimes map[string][]time.Duration

	namespace string
	source    string

	svcNs map[string]string // Used to track services to namespaces mappings
	nsSet map[string]bool   // Used to track created non-default namespaces

	// Mutex to update api latency
	alMutex sync.Mutex
	// Mutex to update svc set
	svcMutex sync.Mutex

	// Action functions
	ActionFuncs map[string]func(*ServiceManager, interface{}) error
}

func NewServiceManager() Manager {
	apt := make(map[string][]time.Duration, 0)

	af := make(map[string]func(*ServiceManager, interface{}) error, 0)

	af[CREATE_ACTION] = (*ServiceManager).Create
	af[DELETE_ACTION] = (*ServiceManager).Delete
	af[LIST_ACTION] = (*ServiceManager).List
	af[GET_ACTION] = (*ServiceManager).Get
	af[UPDATE_ACTION] = (*ServiceManager).Update

	sn := make(map[string]string, 0)
	ns := make(map[string]bool, 0)

	return &ServiceManager{
		apiTimes: apt,

		namespace: apiv1.NamespaceDefault,
		svcNs:     sn,
		nsSet:     ns,

		alMutex:  sync.Mutex{},
		svcMutex: sync.Mutex{},

		ActionFuncs: af,
	}
}

/*
 * This function implements the Init interface and is used to initialize the manager.
 */
func (mgr *ServiceManager) Init(
	kubeConfig *restclient.Config,
	nsName string,
	createNamespace bool,
	maxClients int,
) {
	mgr.namespace = nsName
	mgr.source = perf_util.GetHostnameFromUrl(kubeConfig.Host)

	sharedClient, ce := kubernetes.NewForConfig(kubeConfig)

	if ce != nil {
		panic(ce)
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

	if createNamespace {
		nsSpec := &apiv1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
		_, ne := mgr.client.CoreV1().Namespaces().Create(context.Background(), nsSpec, metav1.CreateOptions{})
		if ne != nil {
			log.Warningf("Fail to create namespace %s, %v", nsName, ne)
		}
	}
}

/*
 * This function implements the CREATE action.
 */
func (mgr *ServiceManager) Create(spec interface{}) error {

	switch s := spec.(type) {
	default:
		log.Errorf("Invalid spec type %T for Service create action.", s)
		return fmt.Errorf("Invalid spec type %T for Service create action.", s)
	case *apiv1.Service:
		tid, _ := strconv.Atoi(s.Labels["tid"])
		cid := tid % len(mgr.clientsets)

		ns := mgr.namespace
		if s.Namespace != "" {
			ns = s.Namespace
			mgr.svcMutex.Lock()
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
			mgr.svcMutex.Unlock()
		}

		startTime := metav1.Now()
		_, err := mgr.clientsets[cid].CoreV1().Services(ns).Create(context.Background(), s, metav1.CreateOptions{})

		latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

		if err != nil {
			return err
		}

		mgr.alMutex.Lock()
		mgr.apiTimes[CREATE_ACTION] = append(mgr.apiTimes[CREATE_ACTION], latency)
		mgr.alMutex.Unlock()

		mgr.svcMutex.Lock()
		mgr.svcNs[s.Name] = ns
		mgr.svcMutex.Unlock()
	}
	return nil
}

/*
 * This function implements the LIST action.
 */
func (mgr *ServiceManager) List(n interface{}) error {

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec type %T for Service list action.", s)
		return fmt.Errorf("Invalid spec type %T for Service list action.", s)
	case ActionSpec:

		options := GetListOptions(s)

		cid := s.Tid % len(mgr.clientsets)

		ns := mgr.namespace
		if s.Namespace != "" {
			ns = s.Namespace
		}

		startTime := metav1.Now()
		ss, err := mgr.clientsets[cid].CoreV1().Services(ns).List(context.Background(), options)
		latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

		if err != nil {
			return err
		}
		log.Infof("Listed %v services", len(ss.Items))

		mgr.alMutex.Lock()
		mgr.apiTimes[LIST_ACTION] = append(mgr.apiTimes[LIST_ACTION], latency)
		mgr.alMutex.Unlock()
	}
	return nil
}

/*
 * This function implements the GET action.
 */
func (mgr *ServiceManager) Get(n interface{}) error {

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec type %T for Service get action.", s)
		return fmt.Errorf("Invalid spec type %T for Service get action.", s)
	case ActionSpec:
		cid := s.Tid % len(mgr.clientsets)

		ns := mgr.namespace
		if s.Namespace != "" {
			ns = s.Namespace
		}

		startTime := metav1.Now()
		ss, err := mgr.clientsets[cid].CoreV1().Services(ns).
			Get(context.Background(), s.Name, metav1.GetOptions{})
		latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

		if err != nil {
			return err
		}

		log.Infof("Got service %v", ss.Name)

		mgr.alMutex.Lock()
		mgr.apiTimes[GET_ACTION] = append(mgr.apiTimes[GET_ACTION], latency)
		mgr.alMutex.Unlock()
	}
	return nil
}

/*
 * This function implements the UPDATE action.
 */
func (mgr *ServiceManager) Update(n interface{}) error {

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec type %T for Service update action.", s)
		return fmt.Errorf("Invalid spec type %T for Service update action.", s)
	case ActionSpec:
		options := GetListOptions(s)

		cid := s.Tid % len(mgr.clientsets)

		ns := mgr.namespace
		if s.Namespace != "" {
			ns = s.Namespace
		}

		svcList, err := mgr.clientsets[cid].CoreV1().Services(ns).List(context.Background(), options)
		if err != nil {
			return err
		}
		svcs := svcList.Items

		for _, currSs := range svcs {
			currSs.Spec.SessionAffinity = apiv1.ServiceAffinityClientIP

			startTime := metav1.Now()
			svc, err := mgr.clientsets[cid].CoreV1().Services(ns).Update(context.Background(), &currSs, metav1.UpdateOptions{})
			latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

			if err != nil {
				return err
			}
			log.Infof("Updated service %v", svc.Name)

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
func (mgr *ServiceManager) Delete(n interface{}) error {
	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec %T for Service delete action.", s)
		return fmt.Errorf("Invalid spec %T for Servic delete action.", s)
	case ActionSpec:
		cid := s.Tid % len(mgr.clientsets)

		ns := mgr.namespace
		if space, ok := mgr.svcNs[s.Name]; ok {
			ns = space
		}

		options := GetListOptions(s)
		svcList, err := mgr.clientsets[cid].CoreV1().Services(ns).List(context.Background(), options)
		if err != nil {
			return err
		}
		svcs := svcList.Items

		for _, currSs := range svcs {
			// Delete the service
			log.Infof("Deleting service %v", currSs.Name)
			startTime := metav1.Now()
			mgr.clientsets[cid].CoreV1().Services(ns).Delete(context.Background(), currSs.Name, metav1.DeleteOptions{})

			latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

			mgr.alMutex.Lock()
			mgr.apiTimes[DELETE_ACTION] = append(mgr.apiTimes[DELETE_ACTION], latency)
			mgr.alMutex.Unlock()

			mgr.svcMutex.Lock()
			// Delete it from the svc set
			_, ok := mgr.svcNs[currSs.Name]

			if ok {
				delete(mgr.svcNs, currSs.Name)
			}
			mgr.svcMutex.Unlock()
		}
	}
	return nil
}

/*
 * This function implements the DeleteAll manager interface. It is used to clean
 * all the resources that are created by the namespace manager.
 */
func (mgr *ServiceManager) DeleteAll() error {

	options := metav1.ListOptions{LabelSelector: labels.SelectorFromSet(
		labels.Set{"app": AppName}).String()}
	ss, err := mgr.client.CoreV1().Services("").List(context.Background(), options)

	if err != nil {
		return err
	}

	if len(ss.Items) > 0 {
		log.Infof("Deleting %v services created by the manager...", len(ss.Items))
		for _, s := range ss.Items {
			mgr.Delete(ActionSpec{
				Name:      s.Name,
				Tid:       0,
				Namespace: mgr.svcNs[s.Name]})
		}
	} else {
		log.Infof("Found no services to delete, maybe they have already been deleted.")
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

	return nil
}

/*
 * This function computes all the metrics and stores the results into the log file.
 */
func (mgr *ServiceManager) LogStats() {

	log.Infof("----------------------------- Service API Call Latencies (ms) " +
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
		log.Infof("%-50v %-10v %-10v %-10v %-10v", m+" service latency: ",
			mid, min, max, p99)
	}
}

func (mgr *ServiceManager) GetResourceName(opNum int, tid int) string {
	return serviceNamePrefix + "oid-" + strconv.Itoa(opNum) + "-tid-" + strconv.Itoa(tid)
}

func (mgr *ServiceManager) SendMetricToWavefront(now time.Time, wfTags []perf_util.WavefrontTag, wavefrontPathDir string, prefix string) {
	var points []perf_util.WavefrontDataPoint
	for m, _ := range mgr.apiTimes {
		mid := float32(mgr.apiTimes[m][len(mgr.apiTimes[m])/2]) / float32(time.Millisecond)
		min := float32(mgr.apiTimes[m][0]) / float32(time.Millisecond)
		max := float32(mgr.apiTimes[m][len(mgr.apiTimes[m])-1]) / float32(time.Millisecond)
		p99 := float32(mgr.apiTimes[m][len(mgr.apiTimes[m])-1-len(mgr.apiTimes[m])/100]) /
			float32(time.Millisecond)
		points = append(points, perf_util.WavefrontDataPoint{"service.apicall." + m + ".median.latency",
			mid, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"service.apicall." + m + ".min.latency",
			min, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"service.apicall." + m + ".max.latency",
			max, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"service.apicall." + m + ".p99.latency",
			p99, now, mgr.source, wfTags})

	}
	var metricLines []string
	for _, point := range points {
		metricLines = append(metricLines, point.String())
		metricLines = append(metricLines, "\n")
	}

	perf_util.WriteDataPoints(now, points, wavefrontPathDir, prefix)
}

func (mgr *ServiceManager) CalculateStats() {
	// This manager has nothing to calculate
}

func (mgr *ServiceManager) CalculateSuccessRate() int {
	// This manager has nothing to calculate
	return 0
}
