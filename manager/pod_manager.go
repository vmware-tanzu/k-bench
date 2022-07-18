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
	//"log"
	//"encoding/json"
	"bytes"
	"fmt"
	osexec "os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	scheme "k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/remotecommand"

	"k-bench/perf_util"
)

const podNamePrefix string = "kbench-pod-"

/*
 * PodManager manages pods actions and stats.
 */
type PodManager struct {
	// This is a shared client
	client *kubernetes.Clientset
	// This is an array of clients used for pod operations
	clientsets []*kubernetes.Clientset
	// Below are used to store the server side timestamps (round to seconds)
	createTimes   map[string]metav1.Time // pod creation timestamp
	scheduleTimes map[string]metav1.Time // timestamp for pod schedule event
	startTimes    map[string]metav1.Time // pod accepted by kubelet, image not pulled
	pulledTimes   map[string]metav1.Time // image pulled time
	runTimes      map[string]metav1.Time // container(s) become running

	// Maps to store client times (based on PodConditionType) with higher precision
	cFirstTimes  map[string]metav1.Time // client sees the first add/update
	cSchedTimes  map[string]metav1.Time // client sees PodScheduled == True
	cInitedTimes map[string]metav1.Time // client sees Initialized == True
	// there is no pulled event handler for client
	cReadyTimes map[string]metav1.Time // client sees Ready == True

	// A map to track the API response time for the supported actions
	apiTimes map[string][]time.Duration

	namespace string // The benchmark's default namespace for pod
	source    string
	config    *restclient.Config

	podNs map[string]string // Used to track pods to namespaces mappings
	nsSet map[string]bool   // Used to track created non-default namespaces

	// Mutex used to update pod startup stats
	statsMutex sync.Mutex
	// Mutex to update pod set
	podMutex sync.Mutex
	// Mutex to update api latency
	alMutex sync.Mutex

	// Action functions
	ActionFuncs map[string]func(*PodManager, interface{}) error

	// Cache related structures
	podController cache.Controller
	podChan       chan struct{}

	podThroughput    float32
	podAvgLatency    float32
	podSrvAvgLatency float32
	negRes           bool

	startTimestamp string

	createToScheLatency, scheToStartLatency   perf_util.OperationLatencyMetric
	startToPulledLatency, pulledToRunLatency  perf_util.OperationLatencyMetric
	createToRunLatency, firstToSchedLatency   perf_util.OperationLatencyMetric
	schedToInitdLatency, initdToReadyLatency  perf_util.OperationLatencyMetric
	firstToReadyLatency, createToReadyLatency perf_util.OperationLatencyMetric
}

func NewPodManager() Manager {
	ctt := make(map[string]metav1.Time, 0)
	sct := make(map[string]metav1.Time, 0)
	stt := make(map[string]metav1.Time, 0)
	put := make(map[string]metav1.Time, 0)
	rut := make(map[string]metav1.Time, 0)

	cft := make(map[string]metav1.Time, 0)
	cst := make(map[string]metav1.Time, 0)
	cit := make(map[string]metav1.Time, 0)
	crt := make(map[string]metav1.Time, 0)

	apt := make(map[string][]time.Duration, 0)

	pn := make(map[string]string, 0)
	ns := make(map[string]bool, 0)
	af := make(map[string]func(*PodManager, interface{}) error, 0)

	af[CREATE_ACTION] = (*PodManager).Create
	af[RUN_ACTION] = (*PodManager).Run
	af[DELETE_ACTION] = (*PodManager).Delete
	af[LIST_ACTION] = (*PodManager).List
	af[GET_ACTION] = (*PodManager).Get
	af[UPDATE_ACTION] = (*PodManager).Update
	af[COPY_ACTION] = (*PodManager).Copy

	pc := make(chan struct{})

	return &PodManager{
		createTimes:   ctt,
		scheduleTimes: sct,
		startTimes:    stt,
		pulledTimes:   put,
		runTimes:      rut,

		cFirstTimes:  cft,
		cSchedTimes:  cst,
		cInitedTimes: cit,
		cReadyTimes:  crt,

		apiTimes: apt,

		namespace: apiv1.NamespaceDefault,
		podNs:     pn,
		nsSet:     ns,

		statsMutex: sync.Mutex{},
		podMutex:   sync.Mutex{},
		alMutex:    sync.Mutex{},

		ActionFuncs: af,
		//podController: nil,
		podChan:        pc,
		startTimestamp: metav1.Now().Format("2006-01-02T15-04-05"),
	}
}

// This function checks the pod's status and updates various timestamps.
func (mgr *PodManager) checkAndUpdate(p *apiv1.Pod) {
	//log.Infof("checkAndUpdate called for %s, status: %v", p.Name, p.Status)

	mgr.statsMutex.Lock()
	defer mgr.statsMutex.Unlock()

	// Store server-side pod start time (acknowledged by Kubelet, but image not pulled)
	if p.Status.StartTime != nil {
		if _, ok := mgr.startTimes[p.Name]; !ok {
			// Store the server side timestamp
			mgr.startTimes[p.Name] = *p.Status.StartTime
		}
	}

	// Store the time when the client gets notified about this pod for the first time
	if _, ok := mgr.cFirstTimes[p.Name]; !ok {
		mgr.cFirstTimes[p.Name] = metav1.Now()
	}

	if p.Status.Phase == apiv1.PodRunning {
		// Store various times upon the first time when client sees a pod is running
		if _, ok := mgr.cReadyTimes[p.Name]; !ok {
			// Record server side timestamp for pod creation
			mgr.createTimes[p.Name] = p.CreationTimestamp

			mgr.cReadyTimes[p.Name] = metav1.Now()

			var lastRunningTime metav1.Time
			for _, cs := range p.Status.ContainerStatuses {
				if cs.State.Running != nil {
					if lastRunningTime.Before(&cs.State.Running.StartedAt) {
						lastRunningTime = cs.State.Running.StartedAt
					}
				}
			}

			if lastRunningTime != metav1.NewTime(time.Time{}) {
				mgr.runTimes[p.Name] = lastRunningTime
				// If cInitedTime has not been recorded, use cSchedTime as an approximation
				if _, ok := mgr.cInitedTimes[p.Name]; !ok {
					if st, stok := mgr.cSchedTimes[p.Name]; stok {
						mgr.cInitedTimes[p.Name] = st
					}
				}
			} else {
				log.Errorf("Pod %v is running, but none of its containers is", p.Name)
			}
		}
	} else if p.Status.Phase == apiv1.PodPending {
		for _, cond := range p.Status.Conditions {
			// Record client time (server time around to seconds) when PodCondition changes
			if cond.Type == apiv1.PodScheduled {
				if _, ok := mgr.cSchedTimes[p.Name]; !ok {
					mgr.cSchedTimes[p.Name] = metav1.Now()
				}
			} else if cond.Type == apiv1.PodInitialized {
				// This should also be the pod's startTime
				if _, ok := mgr.cReadyTimes[p.Name]; ok {
					// PodInitialized callback can be delayed, in such case use cSchedTime
					// as an approximation
					mgr.cInitedTimes[p.Name] = mgr.cSchedTimes[p.Name]
				} else if _, ok := mgr.cInitedTimes[p.Name]; !ok {
					mgr.cInitedTimes[p.Name] = metav1.Now()
				}
				break
			}
		}
	}
}

// This function adds cache with watch list and event handler
func (mgr *PodManager) initCache(resourceType string) {
	_, mgr.podController = cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				options.LabelSelector = labels.SelectorFromSet(
					labels.Set{"app": AppName, "type": resourceType}).String()
				obj, err := mgr.client.CoreV1().Pods("").List(options)
				return runtime.Object(obj), err
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = labels.SelectorFromSet(
					labels.Set{"app": AppName, "type": resourceType}).String()
				return mgr.client.CoreV1().Pods("").Watch(options)
			},
		},
		&apiv1.Pod{},
		0,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				p, ok := obj.(*apiv1.Pod)
				if !ok {
					log.Error("Failed to cast observed object to *v1.Pod.")
				}

				go mgr.checkAndUpdate(p)
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				p, ok := newObj.(*apiv1.Pod)
				if !ok {
					log.Error("Failed to cast observed object to *v1.Pod.")
				}

				go mgr.checkAndUpdate(p)
			},
		},
	)
	//mgr.podController = &controller
	go mgr.podController.Run(mgr.podChan)
}

/*
 * This function updates the stats before deletion
 */
func (mgr *PodManager) UpdateBeforeDeletion(name string, ns string) {
	// Before deletion, make sure schedule and pulled time retrieved for this pod
	// As deletes may happen in multi-threaded section, need to protect the update
	mgr.statsMutex.Lock()
	if _, ok := mgr.scheduleTimes[name]; !ok {
		selector := fields.Set{
			"involvedObject.kind":      "Pod",
			"involvedObject.namespace": ns,
			//"source":                   apiv1.DefaultSchedulerName,
		}.AsSelector().String()
		options := metav1.ListOptions{FieldSelector: selector}
		//TODO: move the below statement out side the lock?
		events, err := mgr.client.CoreV1().Events("").List(options)
		if err != nil {
			log.Error(err)
		} else {
			scheEvents := make(map[string]metav1.Time, 0)
			pulledEvents := make(map[string]metav1.Time, 0)
			var eventTime metav1.Time
			for _, event := range events.Items {
				if event.EventTime.IsZero() {
					eventTime = event.FirstTimestamp
				} else {
					eventTime = metav1.NewTime(time.Unix(event.EventTime.Time.Unix(), 0))
				}

				if event.Source.Component == apiv1.DefaultSchedulerName || event.ReportingController == apiv1.DefaultSchedulerName {
					scheEvents[event.InvolvedObject.Name] = eventTime
				} else if event.Reason == "Pulled" {
					pulledEvents[event.InvolvedObject.Name] = eventTime
				}
			}

			for k := range mgr.createTimes {
				if _, sche_exist := scheEvents[k]; sche_exist {
					mgr.scheduleTimes[k] = scheEvents[k]
				}
				if _, pull_exist := pulledEvents[k]; pull_exist {
					mgr.pulledTimes[k] = pulledEvents[k]
				}
			}
		}
	}
	mgr.statsMutex.Unlock()
}

/*
 * This function implements the Init interface and is used to initialize the manager
 */
func (mgr *PodManager) Init(
	kubeConfig *restclient.Config,
	nsName string,
	createNamespace bool,
	maxClients int,
	resourceType string,
) {
	mgr.namespace = nsName
	mgr.source = perf_util.GetHostnameFromUrl(kubeConfig.Host)
	mgr.config = kubeConfig

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

	if createNamespace {
		nsSpec := &apiv1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
		_, err := mgr.client.CoreV1().Namespaces().Create(nsSpec)
		if err != nil {
			log.Warningf("Fail to create namespace %s, %v", nsName, err)
		} else {
			mgr.nsSet[nsName] = true
		}
	}

	mgr.initCache(resourceType)

}

/*
 * This function implements the CREATE action.
 */
func (mgr *PodManager) Create(spec interface{}) error {

	switch s := spec.(type) {
	default:
		log.Errorf("Invalid spec type %T for Pod create action.", s)
		return fmt.Errorf("Invalid spec type %T for Pod create action.", s)
	case *apiv1.Pod:
		tid, _ := strconv.Atoi(s.Labels["tid"])
		cid := tid % len(mgr.clientsets)

		ns := mgr.namespace
		if s.Namespace != "" {
			ns = s.Namespace
			mgr.podMutex.Lock()
			if _, exist := mgr.nsSet[ns]; !exist && ns != apiv1.NamespaceDefault {
				nsSpec := &apiv1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
				_, err := mgr.client.CoreV1().Namespaces().Create(nsSpec)
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
			mgr.podMutex.Unlock()
		}

		startTime := metav1.Now()
		pod, err := mgr.clientsets[cid].CoreV1().Pods(ns).Create(s)

		latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

		if err != nil {
			return err
		}

		mgr.alMutex.Lock()
		mgr.apiTimes[CREATE_ACTION] = append(mgr.apiTimes[CREATE_ACTION], latency)
		mgr.alMutex.Unlock()

		mgr.podMutex.Lock()
		mgr.podNs[pod.Name] = ns
		mgr.podMutex.Unlock()
	}
	return nil
}

/*
 * This function implements the LIST action.
 */
func (mgr *PodManager) List(n interface{}) error {

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec type %T for Pod list action.", s)
		return fmt.Errorf("Invalid spec type %T for Pod list action.", s)
	case ActionSpec:
		options := GetListOptions(s)

		cid := s.Tid % len(mgr.clientsets)

		ns := mgr.namespace
		if s.Namespace != "" {
			ns = s.Namespace
		}

		startTime := metav1.Now()
		pods, err := mgr.clientsets[cid].CoreV1().Pods(ns).List(options)
		latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

		if err != nil {
			return err
		}
		log.Infof("Listed %v pods", len(pods.Items))

		mgr.alMutex.Lock()
		mgr.apiTimes[LIST_ACTION] = append(mgr.apiTimes[LIST_ACTION], latency)
		mgr.alMutex.Unlock()
	}
	return nil
}

/*
 * This function implements the GET action.
 */
func (mgr *PodManager) Get(n interface{}) error {

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec type %T for Pod get action.", s)
		return fmt.Errorf("Invalid spec type %T for Pod get action.", s)
	case ActionSpec:
		cid := s.Tid % len(mgr.clientsets)
		ns := mgr.namespace
		if s.Namespace != "" {
			ns = s.Namespace
		}
		// Labels (or other filters) are ignored as they do not make sense to GET
		startTime := metav1.Now()
		pod, err := mgr.clientsets[cid].CoreV1().Pods(ns).Get(
			s.Name, metav1.GetOptions{})
		latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

		if err != nil {
			return err
		}

		log.Infof("Got pod %v", pod.Name)

		mgr.alMutex.Lock()
		mgr.apiTimes[GET_ACTION] = append(mgr.apiTimes[GET_ACTION], latency)
		mgr.alMutex.Unlock()
	}
	return nil
}

/*
 * This function implements the RUN action.
 */
func (mgr *PodManager) Run(n interface{}) error {

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec type %T for Pod run action.", s)
		return fmt.Errorf("Invalid spec type %T for Pod run action.", s)
	case RunSpec:
		cid := s.ActionFilter.Tid % len(mgr.clientsets)

		// Find pod(s) using filter first, then name
		options := GetListOptions(s.ActionFilter)

		ns := mgr.namespace
		if s.ActionFilter.Namespace != "" {
			ns = s.ActionFilter.Namespace
		}

		pods := make([]apiv1.Pod, 0)

		podList, err := mgr.clientsets[cid].CoreV1().Pods(ns).List(options)
		if err != nil {
			return err
		}
		pods = podList.Items

		startTime := metav1.Now()
		for _, pod := range pods {
			for _, container := range pod.Spec.Containers {
				log.Infof("Run: Container %v found for pod %v", container.Name,
					pod.Name)

				// TBD - In future, add a container name prefix and filter containers
				// based on this prefix
				runrequest := mgr.clientsets[cid].CoreV1().RESTClient().Post().
					Resource("pods").
					Name(pod.Name).
					Namespace(ns).
					SubResource("exec").
					Param("container", container.Name)
				runrequest.VersionedParams(&apiv1.PodExecOptions{
					Container: container.Name,
					Command:   []string{"/bin/sh", "-c", s.RunCommand},
					Stdin:     false,
					Stdout:    true,
					Stderr:    true,
					TTY:       false,
				}, scheme.ParameterCodec)
				var mystdout, mystderr bytes.Buffer
				exec, err := remotecommand.NewSPDYExecutor(mgr.config,
					"POST", runrequest.URL())
				if err != nil {
					return err
				}

				exec.Stream(remotecommand.StreamOptions{
					Stdin:  nil,
					Stdout: &mystdout,
					Stderr: &mystderr,
					Tty:    false,
				})
				log.Infof("Container %v on pod %v, Run out: %v err: %v",
					container.Name, pod.Name, mystdout.String(),
					mystderr.String())
			}
		}

		latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

		// TBD - invoke s.RunCommand on this pod
		mgr.alMutex.Lock()
		mgr.apiTimes[RUN_ACTION] = append(mgr.apiTimes[RUN_ACTION], latency)
		mgr.alMutex.Unlock()
	}
	return nil
}

/*
 * This function implements the COPY action.
 */
func (mgr *PodManager) Copy(n interface{}) error {

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec type %T for Pod copy action.", s)
		return fmt.Errorf("Invalid spec type %T for Pod copy action.", s)
	case CopySpec:
		cid := s.ActionFilter.Tid % len(mgr.clientsets)

		// Find pod(s) using filter first, then name
		options := GetListOptions(s.ActionFilter)

		ns := mgr.namespace
		if s.ActionFilter.Namespace != "" {
			ns = s.ActionFilter.Namespace
		}

		pods := make([]apiv1.Pod, 0)

		podList, err := mgr.clientsets[cid].CoreV1().Pods(ns).List(options)
		if err != nil {
			return err
		}
		pods = podList.Items

		startTime := metav1.Now()
		for _, pod := range pods {
			// Currently we copy files at pod level (to/from the first container).
			var fromPath, toPath string
			if s.Upload == true {
				fromPath = s.LocalPath
				toPath = pod.Namespace + "/" + pod.Name + ":" + s.ContainerPath
			} else {
				toPath = s.ParentOutDir + "/" + s.LocalPath + "/"
				toPath += mgr.startTimestamp + "/" + pod.Name
				fromPath = pod.Namespace + "/" + pod.Name + ":" + s.ContainerPath
			}
			args := []string{"cp", fromPath, toPath}
			copyr, copye := osexec.Command("kubectl", args...).CombinedOutput()
			if copye != nil {
				log.Errorf("Error copying file(s) for pod: %v", pod.Name)
			} else {
				log.Infof(string(copyr))
			}
		}

		latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

		mgr.alMutex.Lock()
		mgr.apiTimes[COPY_ACTION] = append(mgr.apiTimes[COPY_ACTION], latency)
		mgr.alMutex.Unlock()
	}
	return nil
}

/*
 * This function implements the UPDATE action.
 */
func (mgr *PodManager) Update(n interface{}) error {

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec type %T for Pod update action.", s)
		return fmt.Errorf("Invalid spec type %T for Pod update action.", s)
	case ActionSpec:
		cid := s.Tid % len(mgr.clientsets)

		options := GetListOptions(s)

		ns := mgr.namespace
		if s.Namespace != "" {
			ns = s.Namespace
		}

		pods := make([]apiv1.Pod, 0)

		podList, err := mgr.clientsets[cid].CoreV1().Pods(ns).List(options)
		if err != nil {
			return err
		}
		pods = podList.Items

		newActiveDeadline := int64(10000)

		for _, currPod := range pods {
			currPod.Spec.ActiveDeadlineSeconds = &newActiveDeadline

			startTime := metav1.Now()
			pod, err := mgr.clientsets[cid].CoreV1().Pods(ns).Update(
				&currPod)
			latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

			if err != nil {
				return err
			}
			log.Infof("Updated ActiveDeadlineSeconds for pod %v", pod.Name)

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
func (mgr *PodManager) Delete(n interface{}) error {
	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec %T for Pod delete action.", s)
		return fmt.Errorf("Invalid spec %T for Pod delete action.", s)
	case ActionSpec:
		cid := s.Tid % len(mgr.clientsets)

		options := GetListOptions(s)

		ns := mgr.namespace
		/*if space, ok := mgr.podNs[s.Name]; ok {
			ns = space
		}*/

		if s.Namespace != "" {
			ns = s.Namespace
		}

		pods := make([]apiv1.Pod, 0)

		podList, err := mgr.clientsets[cid].CoreV1().Pods(ns).List(options)
		if err != nil {
			return err
		}
		pods = podList.Items

		for _, currPod := range pods {
			log.Infof("Deleting pod %v", currPod.Name)
			if _, ok := mgr.scheduleTimes[currPod.Name]; !ok {
				mgr.UpdateBeforeDeletion(currPod.Name, ns)
			}

			// Delete the pod
			startTime := metav1.Now()
			mgr.clientsets[cid].CoreV1().Pods(ns).Delete(currPod.Name, nil)
			latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

			mgr.alMutex.Lock()
			mgr.apiTimes[DELETE_ACTION] = append(mgr.apiTimes[DELETE_ACTION], latency)
			mgr.alMutex.Unlock()

			mgr.podMutex.Lock()
			// Delete it from the pod set
			_, ok := mgr.podNs[currPod.Name]

			if ok {
				delete(mgr.podNs, currPod.Name)
			}
			mgr.podMutex.Unlock()
		}
	}
	return nil
}

/*
 * This function implements the DeleteAll manager interface. It is used to clean
 * all the resources that are created by the pod manager.
 */
func (mgr *PodManager) DeleteAll() error {
	if len(mgr.podNs) > 0 {
		log.Infof("Deleting all pods created by the pod manager...")
		for name, _ := range mgr.podNs {
			// Just use tid 0 so that the first client is used to delete all pods
			mgr.Delete(ActionSpec{
				Name: name,
				Tid:  0})
		}
		mgr.podNs = make(map[string]string, 0)
	} else {
		log.Infof("Found no pod to delete, maybe they have already been deleted.")
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

	close(mgr.podChan)
	return nil
}

/*
 * This function returns whether all the created pods become ready
 */
func (mgr *PodManager) IsStable() bool {
	return len(mgr.cReadyTimes) == len(mgr.apiTimes[CREATE_ACTION])
}

/*
 * This function computes all the metrics and stores the results into the log file.
 */
func (mgr *PodManager) LogStats() {
	log.Infof("------------------------------------ Pod Operation Summary " +
		"-----------------------------------")
	log.Infof("%-50v %-10v", "Number of valid pod creation requests:",
		len(mgr.apiTimes[CREATE_ACTION]))
	log.Infof("%-50v %-10v", "Number of created pods:", len(mgr.cFirstTimes))
	log.Infof("%-50v %-10v", "Number of scheduled pods:", len(mgr.cSchedTimes))
	log.Infof("%-50v %-10v", "Number of initialized pods:", len(mgr.cInitedTimes))
	log.Infof("%-50v %-10v", "Number of started pods:", len(mgr.startTimes))
	log.Infof("%-50v %-10v", "Number of running pods:", len(mgr.cReadyTimes))

	log.Infof("%-50v %-10v", "Pod creation throughput (pods/minutes):",
		mgr.podThroughput)
	log.Infof("%-50v %-10v", "Pod creation (client) average latency:",
		mgr.podAvgLatency)
	log.Infof("%-50v %-10v", "Pod creation (server) average latency:",
		mgr.podSrvAvgLatency)

	log.Infof("--------------------------------- Pod Startup Latencies (ms) " +
		"---------------------------------")
	log.Infof("%-50v %-10v %-10v %-10v %-10v", " ", "median", "min", "max", "99%")

	var latency perf_util.OperationLatencyMetric
	latency = mgr.createToScheLatency
	if latency.Valid {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Pod creation latency stats (server): ",
			latency.Latency.Mid, latency.Latency.Min, latency.Latency.Max, latency.Latency.P99)
	} else {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Pod creation latency stats (server): ",
			"---", "---", "---", "---")
	}

	latency = mgr.scheToStartLatency
	if latency.Valid {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Pod scheduling latency stats (server): ",
			latency.Latency.Mid, latency.Latency.Min, latency.Latency.Max, latency.Latency.P99)
	} else {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Pod scheduling latency stats (server): ",
			"---", "---", "---", "---")
	}

	latency = mgr.startToPulledLatency
	if latency.Valid {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Pod image pulling latency stats (server): ",
			latency.Latency.Mid, latency.Latency.Min, latency.Latency.Max, latency.Latency.P99)
	} else {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Pod image pulling latency stats (server): ",
			"---", "---", "---", "---")
	}

	latency = mgr.pulledToRunLatency
	if latency.Valid {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Pod starting latency stats (server): ",
			latency.Latency.Mid, latency.Latency.Min, latency.Latency.Max, latency.Latency.P99)
	} else {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Pod starting latency stats (server): ",
			"---", "---", "---", "---")
	}

	latency = mgr.createToRunLatency
	if latency.Valid {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Pod startup total latency (server): ",
			latency.Latency.Mid, latency.Latency.Min, latency.Latency.Max, latency.Latency.P99)
	} else {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Pod startup total latency (server): ",
			"---", "---", "---", "---")
	}

	latency = mgr.createToReadyLatency
	if latency.Valid {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Pod client-server e2e latency: ",
			latency.Latency.Mid, latency.Latency.Min, latency.Latency.Max, latency.Latency.P99)
	} else {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Pod client-server e2e latency (create-to-ready): ",
			"---", "---", "---", "---")
	}

	latency = mgr.firstToSchedLatency
	if latency.Valid {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Pod scheduling latency stats (client): ",
			latency.Latency.Mid, latency.Latency.Min, latency.Latency.Max, latency.Latency.P99)
	} else {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Pod scheduling latency stats (client): ",
			"---", "---", "---", "---")
	}

	latency = mgr.schedToInitdLatency
	if latency.Valid {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Pod initialization latency on kubelet (client): ",
			latency.Latency.Mid, latency.Latency.Min, latency.Latency.Max, latency.Latency.P99)
	} else {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Pod initialization latency on kubelet (client): ",
			"---", "---", "---", "---")
	}

	latency = mgr.initdToReadyLatency
	if latency.Valid {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Pod starting latency stats (client): ",
			latency.Latency.Mid, latency.Latency.Min, latency.Latency.Max, latency.Latency.P99)
	} else {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Pod starting latency stats (client): ",
			"---", "---", "---", "---")
	}

	latency = mgr.firstToReadyLatency
	if latency.Valid {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Pod startup total latency (client): ",
			latency.Latency.Mid, latency.Latency.Min, latency.Latency.Max, latency.Latency.P99)
	} else {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Pod startup total latency (client): ",
			"---", "---", "---", "---")
	}

	log.Infof("--------------------------------- Pod API Call Latencies (ms) " +
		"--------------------------------")
	log.Infof("%-50v %-10v %-10v %-10v %-10v", " ", "median", "min", "max", "99%")

	var mid, min, max, p99 float32
	for m, _ := range mgr.apiTimes {
		mid = float32(mgr.apiTimes[m][len(mgr.apiTimes[m])/2]) / float32(time.Millisecond)
		min = float32(mgr.apiTimes[m][0]) / float32(time.Millisecond)
		max = float32(mgr.apiTimes[m][len(mgr.apiTimes[m])-1]) / float32(time.Millisecond)
		p99 = float32(mgr.apiTimes[m][len(mgr.apiTimes[m])-1-len(mgr.apiTimes[m])/100]) /
			float32(time.Millisecond)
		log.Infof("%-50v %-10v %-10v %-10v %-10v", m+" pod latency: ", mid, min, max, p99)
	}

	if mgr.scheToStartLatency.Latency.Mid < 0 {
		log.Warning("There might be time skew between server and nodes, " +
			"server side metrics such as scheduling latency stats (server) above is negative.")
	}

	// If we see negative server side results or server-client latency is larger than client latency by more than 3x
	if mgr.negRes || mgr.createToReadyLatency.Latency.Mid/3 > mgr.firstToReadyLatency.Latency.Mid {
		log.Warning("There might be time skew between client and server, " +
			"and certain results (e.g., client-server e2e latency) above " +
			"may have been affected.")
	}

}

func (mgr *PodManager) GetResourceName(userPodPrefix string, opNum int, tid int) string {
	if userPodPrefix == "" {
		return podNamePrefix + "oid-" + strconv.Itoa(opNum) + "-tid-" + strconv.Itoa(tid)
	} else {
		return userPodPrefix + "-" + podNamePrefix + "oid-" + strconv.Itoa(opNum) + "-tid-" + strconv.Itoa(tid)
	}
}

func (mgr *PodManager) SendMetricToWavefront(
	now time.Time,
	wfTags []perf_util.WavefrontTag,
	wavefrontPathDir string,
	prefix string) {
	var points []perf_util.WavefrontDataPoint

	points = append(points, perf_util.WavefrontDataPoint{"pod.creation.throuput",
		mgr.podThroughput, now, mgr.source, wfTags})

	//create to sched
	if mgr.createToScheLatency.Valid {
		points = append(points, perf_util.WavefrontDataPoint{"pod.server.creation.median.latency",
			mgr.createToScheLatency.Latency.Mid, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.server.creation.min.latency",
			mgr.createToScheLatency.Latency.Min, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.server.creation.max.latency",
			mgr.createToScheLatency.Latency.Max, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.server.creation.p99.latency",
			mgr.createToScheLatency.Latency.P99, now, mgr.source, wfTags})
	}

	if mgr.scheToStartLatency.Valid {
		points = append(points, perf_util.WavefrontDataPoint{"pod.server.scheduling.median.latency",
			mgr.scheToStartLatency.Latency.Mid, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.server.scheduling.min.latency",
			mgr.scheToStartLatency.Latency.Min, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.server.scheduling.max.latency",
			mgr.scheToStartLatency.Latency.Max, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.server.scheduling.p99.latency",
			mgr.scheToStartLatency.Latency.P99, now, mgr.source, wfTags})
	}

	if mgr.startToPulledLatency.Valid {
		points = append(points, perf_util.WavefrontDataPoint{"pod.server.image.pulling.median.latency",
			mgr.startToPulledLatency.Latency.Mid, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.server.image.pulling.min.latency",
			mgr.startToPulledLatency.Latency.Min, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.server.image.pulling.max.latency",
			mgr.startToPulledLatency.Latency.Max, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.server.image.pulling.p99.latency",
			mgr.startToPulledLatency.Latency.P99, now, mgr.source, wfTags})
	}

	if mgr.pulledToRunLatency.Valid {
		points = append(points, perf_util.WavefrontDataPoint{"pod.server.starting.median.latency",
			mgr.pulledToRunLatency.Latency.Mid, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.server.starting.min.latency",
			mgr.pulledToRunLatency.Latency.Min, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.server.starting.max.latency",
			mgr.pulledToRunLatency.Latency.Max, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.server.starting.p99.latency",
			mgr.pulledToRunLatency.Latency.P99, now, mgr.source, wfTags})
	}

	if mgr.createToRunLatency.Valid {
		points = append(points, perf_util.WavefrontDataPoint{"pod.server.startup.total.median.latency",
			mgr.createToRunLatency.Latency.Mid, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.server.startup.total.min.latency",
			mgr.createToRunLatency.Latency.Min, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.server.startup.total.max.latency",
			mgr.createToRunLatency.Latency.Max, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.server.startup.total.p99.latency",
			mgr.createToRunLatency.Latency.P99, now, mgr.source, wfTags})
	}

	if mgr.createToReadyLatency.Valid {
		points = append(points, perf_util.WavefrontDataPoint{"pod.clientServer.e2e.median.latency",
			mgr.createToReadyLatency.Latency.Mid, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.clientServer.e2e.min.latency",
			mgr.createToReadyLatency.Latency.Min, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.clientServer.e2e.max.latency",
			mgr.createToReadyLatency.Latency.Max, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.clientServer.e2e.p99.latency",
			mgr.createToReadyLatency.Latency.P99, now, mgr.source, wfTags})
	}

	if mgr.firstToSchedLatency.Valid {
		points = append(points, perf_util.WavefrontDataPoint{"pod.client.scheduling.median.latency",
			mgr.firstToSchedLatency.Latency.Mid, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.client.scheduling.min.latency",
			mgr.firstToSchedLatency.Latency.Min, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.client.scheduling.max.latency",
			mgr.firstToSchedLatency.Latency.Max, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.client.scheduling.p99.latency",
			mgr.firstToSchedLatency.Latency.P99, now, mgr.source, wfTags})
	}

	if mgr.schedToInitdLatency.Valid {
		points = append(points, perf_util.WavefrontDataPoint{"pod.client.kubelet.initialize.median.latency",
			mgr.schedToInitdLatency.Latency.Mid, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.client.kubelet.initialize.min.latency",
			mgr.schedToInitdLatency.Latency.Min, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.client.kubelet.initialize.max.latency",
			mgr.schedToInitdLatency.Latency.Max, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.client.kubelet.initialize.p99.latency",
			mgr.schedToInitdLatency.Latency.P99, now, mgr.source, wfTags})
	}

	if mgr.initdToReadyLatency.Valid {
		points = append(points, perf_util.WavefrontDataPoint{"pod.client.starting.median.latency",
			mgr.initdToReadyLatency.Latency.Mid, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.client.starting.min.latency",
			mgr.initdToReadyLatency.Latency.Min, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.client.starting.max.latency",
			mgr.initdToReadyLatency.Latency.Max, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.client.starting.p99.latency",
			mgr.initdToReadyLatency.Latency.P99, now, mgr.source, wfTags})
	}

	if mgr.firstToReadyLatency.Valid {
		points = append(points, perf_util.WavefrontDataPoint{"pod.client.startup.total.median.latency",
			mgr.firstToReadyLatency.Latency.Mid, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.client.startup.total.min.latency",
			mgr.firstToReadyLatency.Latency.Min, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.client.startup.total.max.latency",
			mgr.firstToReadyLatency.Latency.Max, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.client.startup.total.p99.latency",
			mgr.firstToReadyLatency.Latency.P99, now, mgr.source, wfTags})
	}

	var mid, min, max, p99 float32
	for m, _ := range mgr.apiTimes {
		mid = float32(mgr.apiTimes[m][len(mgr.apiTimes[m])/2]) / float32(time.Millisecond)
		min = float32(mgr.apiTimes[m][0]) / float32(time.Millisecond)
		max = float32(mgr.apiTimes[m][len(mgr.apiTimes[m])-1]) / float32(time.Millisecond)
		p99 = float32(mgr.apiTimes[m][len(mgr.apiTimes[m])-1-len(mgr.apiTimes[m])/100]) /
			float32(time.Millisecond)

		points = append(points, perf_util.WavefrontDataPoint{"pod.apicall." + m + ".median.latency",
			mid, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.apicall." + m + ".min.latency",
			min, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.apicall." + m + ".max.latency",
			max, now, mgr.source, wfTags})
		points = append(points, perf_util.WavefrontDataPoint{"pod.apicall." + m + ".p99.latency",
			p99, now, mgr.source, wfTags})
	}
	perf_util.WriteDataPoints(now, points, wavefrontPathDir, prefix)
}

// Get op num given pod name
func (mgr *PodManager) getOpNum(name string) int {
	//start := len(podNamePrefix)
	start := strings.LastIndex(name, "-oid-") + len("-oid-")
	end := strings.LastIndex(name, "-tid-")

	opStr := name[start:end]
	res, err := strconv.Atoi(opStr)
	if err != nil {
		return -1
	}
	return res
}

func (mgr *PodManager) CalculateStats() {
	latPerOp := make(map[int][]float32, 0)
	var totalLat float32
	var totalSrvLat float32
	totalLat = 0.0
	totalSrvLat = 0.0
	podCount := 0
	// The below loop groups the latency by operation
	for p, ct := range mgr.cReadyTimes {
		opn := mgr.getOpNum(p)
		if opn == -1 {
			continue
		}
		nl := float32(ct.Time.Sub(mgr.cFirstTimes[p].Time)) / float32(time.Second)
		latPerOp[opn] = append(latPerOp[opn], nl)
		totalLat += nl
		podCount += 1
	}

	var accStartTime float32
	accStartTime = 0.0
	accPods := 0

	for opn, _ := range latPerOp {
		sort.Slice(latPerOp[opn],
			func(i, j int) bool { return latPerOp[opn][i] < latPerOp[opn][j] })

		curLen := len(latPerOp[opn])
		accStartTime += float32(latPerOp[opn][curLen/2])
		accPods += (curLen + 1) / 2
	}

	mgr.podAvgLatency = totalLat / float32(podCount)
	mgr.podThroughput = float32(accPods) * float32(60) / accStartTime

	createToSche := make([]time.Duration, 0)
	scheToStart := make([]time.Duration, 0)
	startToPulled := make([]time.Duration, 0)
	pulledToRun := make([]time.Duration, 0)
	createToRun := make([]time.Duration, 0)

	firstToSched := make([]time.Duration, 0)
	schedToInitd := make([]time.Duration, 0)
	initdToReady := make([]time.Duration, 0)
	firstToReady := make([]time.Duration, 0)

	createToReady := make([]time.Duration, 0)

	for p, ct := range mgr.createTimes {
		if st, ok := mgr.scheduleTimes[p]; ok {
			createToSche = append(createToSche, st.Time.Sub(ct.Time))
		}
	}
	for p, ct := range mgr.scheduleTimes {
		if st, ok := mgr.startTimes[p]; ok {
			scheToStart = append(scheToStart, st.Time.Sub(ct.Time))
		}
	}
	for p, ct := range mgr.startTimes {
		if st, ok := mgr.pulledTimes[p]; ok {
			startToPulled = append(startToPulled, st.Time.Sub(ct.Time))
		}
	}
	for p, ct := range mgr.pulledTimes {
		if st, ok := mgr.runTimes[p]; ok {
			pulledToRun = append(pulledToRun, st.Time.Sub(ct.Time))
		}
	}
	for p, ct := range mgr.runTimes {
		if st, ok := mgr.createTimes[p]; ok {
			createToRun = append(createToRun, ct.Time.Sub(st.Time))
		}
	}

	for p, ct := range mgr.cFirstTimes {
		if st, ok := mgr.cSchedTimes[p]; ok {
			firstToSched = append(firstToSched, st.Time.Sub(ct.Time).
				Round(time.Microsecond))
		}
	}
	for p, ct := range mgr.cSchedTimes {
		if st, ok := mgr.cInitedTimes[p]; ok {
			schedToInitd = append(schedToInitd, st.Time.Sub(ct.Time).
				Round(time.Microsecond))
		}
	}
	for p, ct := range mgr.cInitedTimes {
		if st, ok := mgr.cReadyTimes[p]; ok {
			initdToReady = append(initdToReady, st.Time.Sub(ct.Time).
				Round(time.Microsecond))
		}
	}
	for p, ct := range mgr.cReadyTimes {
		if st, ok := mgr.cFirstTimes[p]; ok {
			firstToReady = append(firstToReady, ct.Time.Sub(st.Time).
				Round(time.Microsecond))
		}
	}

	for p, ct := range mgr.cReadyTimes {
		if st, ok := mgr.createTimes[p]; ok {
			createToReady = append(createToReady, ct.Time.Sub(st.Time).
				Round(time.Microsecond))
		}
	}

	sort.Slice(createToSche,
		func(i, j int) bool { return createToSche[i] < createToSche[j] })
	sort.Slice(scheToStart,
		func(i, j int) bool { return scheToStart[i] < scheToStart[j] })
	sort.Slice(startToPulled,
		func(i, j int) bool { return startToPulled[i] < startToPulled[j] })
	sort.Slice(pulledToRun,
		func(i, j int) bool { return pulledToRun[i] < pulledToRun[j] })
	sort.Slice(createToRun,
		func(i, j int) bool { return createToRun[i] < createToRun[j] })
	sort.Slice(firstToSched,
		func(i, j int) bool { return firstToSched[i] < firstToSched[j] })
	sort.Slice(schedToInitd,
		func(i, j int) bool { return schedToInitd[i] < schedToInitd[j] })
	sort.Slice(initdToReady,
		func(i, j int) bool { return initdToReady[i] < initdToReady[j] })
	sort.Slice(firstToReady,
		func(i, j int) bool { return firstToReady[i] < firstToReady[j] })
	sort.Slice(createToReady,
		func(i, j int) bool { return createToReady[i] < createToReady[j] })

	for _, ct := range createToRun {
		totalSrvLat += float32(ct) / float32(time.Second)
	}

	mgr.podSrvAvgLatency = totalSrvLat / float32(podCount)

	var mid, min, max, p99 float32

	if len(createToSche) > 0 {
		mid = float32(createToSche[len(createToSche)/2]) / float32(time.Millisecond)
		min = float32(createToSche[0]) / float32(time.Millisecond)
		max = float32(createToSche[len(createToSche)-1]) / float32(time.Millisecond)
		p99 = float32(createToSche[len(createToSche)-1-len(createToSche)/100]) /
			float32(time.Millisecond)
		mgr.createToScheLatency.Valid = true
		mgr.createToScheLatency.Latency = perf_util.LatencyMetric{mid, min, max, p99}
	}

	if len(scheToStart) > 0 {
		mid = float32(scheToStart[len(scheToStart)/2]) / float32(time.Millisecond)
		min = float32(scheToStart[0]) / float32(time.Millisecond)
		max = float32(scheToStart[len(scheToStart)-1]) / float32(time.Millisecond)
		p99 = float32(scheToStart[len(scheToStart)-1-len(scheToStart)/100]) /
			float32(time.Millisecond)
		mgr.scheToStartLatency.Valid = true
		mgr.scheToStartLatency.Latency = perf_util.LatencyMetric{mid, min, max, p99}
	}

	if len(startToPulled) > 0 {
		mid = float32(startToPulled[len(startToPulled)/2]) / float32(time.Millisecond)
		min = float32(startToPulled[0]) / float32(time.Millisecond)
		max = float32(startToPulled[len(startToPulled)-1]) / float32(time.Millisecond)
		p99 = float32(startToPulled[len(startToPulled)-1-len(startToPulled)/100]) /
			float32(time.Millisecond)
		mgr.startToPulledLatency.Valid = true
		mgr.startToPulledLatency.Latency = perf_util.LatencyMetric{mid, min, max, p99}
	}

	if len(pulledToRun) > 0 {
		mid = float32(pulledToRun[len(pulledToRun)/2]) / float32(time.Millisecond)
		min = float32(pulledToRun[0]) / float32(time.Millisecond)
		max = float32(pulledToRun[len(pulledToRun)-1]) / float32(time.Millisecond)
		p99 = float32(pulledToRun[len(pulledToRun)-1-len(pulledToRun)/100]) /
			float32(time.Millisecond)
		mgr.pulledToRunLatency.Valid = true
		mgr.pulledToRunLatency.Latency = perf_util.LatencyMetric{mid, min, max, p99}
	}

	if len(createToRun) > 0 {
		mid = float32(createToRun[len(createToRun)/2]) / float32(time.Millisecond)
		min = float32(createToRun[0]) / float32(time.Millisecond)
		max = float32(createToRun[len(createToRun)-1]) / float32(time.Millisecond)
		p99 = float32(createToRun[len(createToRun)-1-len(createToRun)/100]) /
			float32(time.Millisecond)
		mgr.createToRunLatency.Valid = true
		mgr.createToRunLatency.Latency = perf_util.LatencyMetric{mid, min, max, p99}
	}

	if len(createToReady) > 0 {
		mid = float32(createToReady[len(createToReady)/2]) / float32(time.Millisecond)
		min = float32(createToReady[0]) / float32(time.Millisecond)
		max = float32(createToReady[len(createToReady)-1]) / float32(time.Millisecond)
		p99 = float32(createToReady[len(createToReady)-1-len(createToReady)/100]) /
			float32(time.Millisecond)
		if mid < 0 || min < 0 {
			mgr.negRes = true
		}
		mgr.createToReadyLatency.Valid = true
		mgr.createToReadyLatency.Latency = perf_util.LatencyMetric{mid, min, max, p99}
	}

	if len(firstToSched) > 0 {
		mid = float32(firstToSched[len(firstToSched)/2]) / float32(time.Millisecond)
		min = float32(firstToSched[0]) / float32(time.Millisecond)
		max = float32(firstToSched[len(firstToSched)-1]) / float32(time.Millisecond)
		p99 = float32(firstToSched[len(firstToSched)-1-len(firstToSched)/100]) /
			float32(time.Millisecond)
		mgr.firstToSchedLatency.Valid = true
		mgr.firstToSchedLatency.Latency = perf_util.LatencyMetric{mid, min, max, p99}
	}

	if len(schedToInitd) > 0 {
		mid = float32(schedToInitd[len(schedToInitd)/2]) / float32(time.Millisecond)
		min = float32(schedToInitd[0]) / float32(time.Millisecond)
		max = float32(schedToInitd[len(schedToInitd)-1]) / float32(time.Millisecond)
		p99 = float32(schedToInitd[len(schedToInitd)-1-len(schedToInitd)/100]) /
			float32(time.Millisecond)
		mgr.schedToInitdLatency.Valid = true
		mgr.schedToInitdLatency.Latency = perf_util.LatencyMetric{mid, min, max, p99}
	}

	if len(initdToReady) > 0 {
		mid = float32(initdToReady[len(initdToReady)/2]) / float32(time.Millisecond)
		min = float32(initdToReady[0]) / float32(time.Millisecond)
		max = float32(initdToReady[len(initdToReady)-1]) / float32(time.Millisecond)
		p99 = float32(initdToReady[len(initdToReady)-1-len(initdToReady)/100]) /
			float32(time.Millisecond)
		mgr.initdToReadyLatency.Valid = true
		mgr.initdToReadyLatency.Latency = perf_util.LatencyMetric{mid, min, max, p99}
	}

	if len(firstToReady) > 0 {
		mid = float32(firstToReady[len(firstToReady)/2]) / float32(time.Millisecond)
		min = float32(firstToReady[0]) / float32(time.Millisecond)
		max = float32(firstToReady[len(firstToReady)-1]) / float32(time.Millisecond)
		p99 = float32(firstToReady[len(firstToReady)-1-len(firstToReady)/100]) /
			float32(time.Millisecond)
		mgr.firstToReadyLatency.Valid = true
		mgr.firstToReadyLatency.Latency = perf_util.LatencyMetric{mid, min, max, p99}
	}

	for m, _ := range mgr.apiTimes {
		sort.Slice(mgr.apiTimes[m],
			func(i, j int) bool { return mgr.apiTimes[m][i] < mgr.apiTimes[m][j] })
	}

}

func (mgr *PodManager) CalculateSuccessRate() int {
	if len(mgr.cFirstTimes) == 0 {
		return 0
	}
	return len(mgr.cReadyTimes) * 100 / len(mgr.cFirstTimes)
}
