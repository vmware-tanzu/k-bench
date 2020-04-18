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
	v1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/dynamic"
	"sort"
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
	"k8s.io/client-go/kubernetes"
)

const benchPrefix string = "kbench-"

/*
 * ResourceManager manages resource (of any kind) actions and stats.
 */
type ResourceManager struct {
	// This is a shared client
	client *kubernetes.Clientset
	// Below are clients for future uses:
	dynclients  []dynamic.Interface
	restclients map[string][]restclient.Interface // string here is the GroupVersion.String()
	// This is an array of clients used for service operations
	clientsets []*kubernetes.Clientset

	kubeConfig *restclient.Config

	// A map to track the API response time for the supported resource kinds and actions
	apiTimes map[string]map[string][]time.Duration

	namespace string
	source    string

	resNs map[string]string // Used to track resources to namespaces mappings
	nsSet map[string]bool   // Used to track created non-default namespaces

	// Mutex to update api latency
	alMutex sync.Mutex
	// Mutex to update svc set
	resMutex sync.Mutex

	// Action functions
	ActionFuncs map[string]func(*ResourceManager, interface{}) error
}

func NewResourceManager() Manager {
	apt := make(map[string]map[string][]time.Duration, 0)

	af := make(map[string]func(*ResourceManager, interface{}) error, 0)

	af[CREATE_ACTION] = (*ResourceManager).Create
	af[DELETE_ACTION] = (*ResourceManager).Delete
	af[LIST_ACTION] = (*ResourceManager).List
	af[GET_ACTION] = (*ResourceManager).Get

	rn := make(map[string]string, 0)
	ns := make(map[string]bool, 0)

	return &ResourceManager{
		apiTimes: apt,

		namespace: apiv1.NamespaceDefault,
		resNs:     rn,
		nsSet:     ns,

		alMutex:  sync.Mutex{},
		resMutex: sync.Mutex{},

		ActionFuncs: af,
	}
}

/*
 * This function implements the Init interface and is used to initialize the manager.
 */
func (mgr *ResourceManager) Init(
	kubeConfig *restclient.Config,
	nsName string,
	createNamespace bool,
	maxClients int,
) {
	mgr.namespace = nsName
	mgr.source = perf_util.GetHostnameFromUrl(kubeConfig.Host)

	mgr.kubeConfig = kubeConfig

	sharedClient, ce := kubernetes.NewForConfig(kubeConfig)

	if ce != nil {
		panic(ce)
	}

	mgr.client = sharedClient

	mgr.dynclients = make([]dynamic.Interface, maxClients)

	for i := 0; i < maxClients; i++ {
		client, ce := dynamic.NewForConfig(kubeConfig)

		if ce != nil {
			panic(ce)
		}

		mgr.dynclients[i] = client
	}

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
		_, ne := mgr.client.CoreV1().Namespaces().Create(nsSpec)
		if ne != nil {
			log.Warningf("Fail to create namespace %s, %v", nsName, ne)
		}
	}
}

/*
 * This function implements the CREATE action.
 */
func (mgr *ResourceManager) Create(spec interface{}) error {
	ro := spec
	metadata, me := meta.Accessor(ro)
	if me != nil {
		log.Errorf("Failed to get metadata accessor for resource creation")
		return me
	}
	objLabels := metadata.GetLabels()
	objNamespace := metadata.GetNamespace()
	objName := metadata.GetName()

	var kind string

	ns := mgr.namespace
	if objNamespace != "" {
		ns = objNamespace
		mgr.resMutex.Lock()
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
		mgr.resMutex.Unlock()
	}

	tid, _ := strconv.Atoi(objLabels["tid"])
	cid := tid % len(mgr.clientsets)

	var startTime metav1.Time
	var latency time.Duration
	var err error

	startTime = metav1.Now()

	switch obj := spec.(type) {
	default:
		log.Errorf("Invalid spec type %T for resource create action.", obj)
		return fmt.Errorf("Invalid spec type %T for resource create action.", obj)
	case *apiv1.Pod:
		_, err = mgr.clientsets[cid].CoreV1().Pods(ns).Create(obj)
		kind = POD

	case *apiv1.Namespace:
		_, err = mgr.clientsets[cid].CoreV1().Namespaces().Create(obj)
		kind = NAMESPACE
	case *apiv1.Service:
		_, err = mgr.clientsets[cid].CoreV1().Services(ns).Create(obj)
		kind = SERVICE
	case *apiv1.ConfigMap:
		_, err = mgr.clientsets[cid].CoreV1().ConfigMaps(ns).Create(obj)
		kind = CONFIG_MAP
	case *apiv1.Endpoints:
		_, err = mgr.clientsets[cid].CoreV1().Endpoints(ns).Create(obj)
		kind = ENDPOINTS
	case *apiv1.Event:
		_, err = mgr.clientsets[cid].CoreV1().Events(ns).Create(obj)
		kind = EVENT
	case *apiv1.ComponentStatus:
		log.Errorf("Create action is not supported for %s.", COMPONENT_STATUS)
		return fmt.Errorf("create action not supported on the resource")
	case *apiv1.Node:
		_, err = mgr.clientsets[cid].CoreV1().Nodes().Create(obj)
		kind = NODE
	case *apiv1.LimitRange:
		_, err = mgr.clientsets[cid].CoreV1().LimitRanges(ns).Create(obj)
		kind = LIMIT_RANGE
	case *apiv1.PersistentVolumeClaim:
		_, err = mgr.clientsets[cid].CoreV1().PersistentVolumeClaims(ns).Create(obj)
		kind = PERSISTENT_VOLUME_CLAIM
	case *apiv1.PersistentVolume:
		_, err = mgr.clientsets[cid].CoreV1().PersistentVolumes().Create(obj)
		kind = PERSISTENT_VOLUME
	case *apiv1.PodTemplate:
		_, err = mgr.clientsets[cid].CoreV1().PodTemplates(ns).Create(obj)
		kind = POD_TEMPLATE
	case *apiv1.ResourceQuota:
		_, err = mgr.clientsets[cid].CoreV1().ResourceQuotas(ns).Create(obj)
		kind = RESOURCE_QUOTA
	case *apiv1.Secret:
		_, err = mgr.clientsets[cid].CoreV1().Secrets(ns).Create(obj)
		kind = SECRET
	case *apiv1.ServiceAccount:
		_, err = mgr.clientsets[cid].CoreV1().ServiceAccounts(ns).Create(obj)
		kind = SERVICE_ACCOUNT
	case *v1.Role:
		_, err = mgr.clientsets[cid].RbacV1().Roles(ns).Create(obj)
		kind = ROLE
	case *v1.RoleBinding:
		_, err = mgr.clientsets[cid].RbacV1().RoleBindings(ns).Create(obj)
		kind = ROLE_BINDING
	case *v1.ClusterRole:
		_, err = mgr.clientsets[cid].RbacV1().ClusterRoles().Create(obj)
		kind = CLUSTER_ROLE
	case *v1.ClusterRoleBinding:
		_, err = mgr.clientsets[cid].RbacV1().ClusterRoleBindings().Create(obj)
		kind = CLUSTER_ROLE_BINDING
	}

	latency = metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

	if err != nil {
		return err
	}

	log.Infof("Created the resource as a %s", kind)

	mgr.alMutex.Lock()
	if mgr.apiTimes[kind] == nil {
		mgr.apiTimes[kind] = make(map[string][]time.Duration, 0)
	}
	mgr.apiTimes[kind][CREATE_ACTION] = append(mgr.apiTimes[kind][CREATE_ACTION], latency)
	mgr.alMutex.Unlock()

	mgr.resMutex.Lock()
	mgr.resNs[objName] = ns
	mgr.resMutex.Unlock()
	return nil
}

/*
 * This function implements the LIST action.
 */
func (mgr *ResourceManager) List(n interface{}) error {

	var kind string
	ns := mgr.namespace

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec %T for resource delete action.", s)
		return fmt.Errorf("Invalid spec %T for resource delete action.", s)
	case ActionSpec:
		cid := s.Tid % len(mgr.clientsets)
		if s.Namespace != "" {
			ns = s.Namespace
		}

		kind = s.Kind

		options := GetListOptions(s)

		var latency time.Duration
		startTime := metav1.Now()
		var l int

		switch kind {
		default:
			log.Errorf("Invalid kind %T for resource delete action.", kind)
		case POD:
			resList, _ := mgr.clientsets[cid].CoreV1().Pods(ns).List(options)
			l = len(resList.Items)
			kind = POD
		case SERVICE:
			resList, _ := mgr.clientsets[cid].CoreV1().Services(ns).List(options)
			l = len(resList.Items)
			kind = SERVICE
		case CONFIG_MAP:
			resList, _ := mgr.clientsets[cid].CoreV1().ConfigMaps(ns).List(options)
			l = len(resList.Items)
			kind = CONFIG_MAP
		case ENDPOINTS:
			resList, _ := mgr.clientsets[cid].CoreV1().Endpoints(ns).List(options)
			l = len(resList.Items)
			kind = ENDPOINTS
		case EVENT:
			resList, _ := mgr.clientsets[cid].CoreV1().Events(ns).List(options)
			l = len(resList.Items)
			kind = EVENT
		case COMPONENT_STATUS:
			resList, _ := mgr.clientsets[cid].CoreV1().ComponentStatuses().List(options)
			l = len(resList.Items)
			kind = COMPONENT_STATUS
		case NODE:
			resList, _ := mgr.clientsets[cid].CoreV1().Nodes().List(options)
			l = len(resList.Items)
			kind = NODE
		case LIMIT_RANGE:
			resList, _ := mgr.clientsets[cid].CoreV1().LimitRanges(ns).List(options)
			l = len(resList.Items)
			kind = LIMIT_RANGE
		case PERSISTENT_VOLUME_CLAIM:
			resList, _ := mgr.clientsets[cid].CoreV1().PersistentVolumeClaims(ns).List(options)
			l = len(resList.Items)
			kind = PERSISTENT_VOLUME_CLAIM
		case PERSISTENT_VOLUME:
			resList, _ := mgr.clientsets[cid].CoreV1().PersistentVolumes().List(options)
			l = len(resList.Items)
			kind = PERSISTENT_VOLUME
		case POD_TEMPLATE:
			resList, _ := mgr.clientsets[cid].CoreV1().PodTemplates(ns).List(options)
			l = len(resList.Items)
			kind = POD_TEMPLATE
		case RESOURCE_QUOTA:
			resList, _ := mgr.clientsets[cid].CoreV1().ResourceQuotas(ns).List(options)
			l = len(resList.Items)
			kind = RESOURCE_QUOTA
		case SECRET:
			resList, _ := mgr.clientsets[cid].CoreV1().Secrets(ns).List(options)
			l = len(resList.Items)
			kind = SECRET
		case SERVICE_ACCOUNT:
			resList, _ := mgr.clientsets[cid].CoreV1().ServiceAccounts(ns).List(options)
			l = len(resList.Items)
			kind = SERVICE_ACCOUNT
		case ROLE:
			resList, _ := mgr.clientsets[cid].RbacV1().Roles(ns).List(options)
			l = len(resList.Items)
			kind = ROLE
		case ROLE_BINDING:
			resList, _ := mgr.clientsets[cid].RbacV1().RoleBindings(ns).List(options)
			l = len(resList.Items)
			kind = ROLE_BINDING
		case CLUSTER_ROLE:
			resList, _ := mgr.clientsets[cid].RbacV1().ClusterRoles().List(options)
			l = len(resList.Items)
			kind = CLUSTER_ROLE
		case CLUSTER_ROLE_BINDING:
			resList, _ := mgr.clientsets[cid].RbacV1().ClusterRoleBindings().List(options)
			l = len(resList.Items)
			kind = CLUSTER_ROLE_BINDING
		}

		latency = metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)
		log.Infof("List %v %s", l, kind)

		mgr.alMutex.Lock()
		if mgr.apiTimes[kind] == nil {
			mgr.apiTimes[kind] = make(map[string][]time.Duration, 0)
		}
		// Update latency stats
		mgr.apiTimes[kind][LIST_ACTION] = append(mgr.apiTimes[kind][LIST_ACTION], latency)
		mgr.alMutex.Unlock()
	}
	return nil
}

/*
 * This function implements the GET action.
 */
func (mgr *ResourceManager) Get(n interface{}) error {
	var kind string

	ns := mgr.namespace

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec %T for resource delete action.", s)
		return fmt.Errorf("Invalid spec %T for resource delete action.", s)
	case ActionSpec:
		cid := s.Tid % len(mgr.clientsets)
		if s.Namespace != "" {
			ns = s.Namespace
		}
		if space, ok := mgr.resNs[s.Name]; ok {
			ns = space
		}

		kind = s.Kind

		var latency time.Duration
		startTime := metav1.Now()

		switch kind {
		default:
			log.Errorf("Invalid kind %T for resource delete action.", kind)
		case POD:
			mgr.clientsets[cid].CoreV1().Pods(ns).Get(s.Name, metav1.GetOptions{})
			kind = POD
		case SERVICE:
			mgr.clientsets[cid].CoreV1().Services(ns).Get(s.Name, metav1.GetOptions{})
			kind = SERVICE
		case CONFIG_MAP:
			mgr.clientsets[cid].CoreV1().ConfigMaps(ns).Get(s.Name, metav1.GetOptions{})
			kind = CONFIG_MAP
		case ENDPOINTS:
			mgr.clientsets[cid].CoreV1().Endpoints(ns).Get(s.Name, metav1.GetOptions{})
			kind = ENDPOINTS
		case EVENT:
			mgr.clientsets[cid].CoreV1().Events(ns).Get(s.Name, metav1.GetOptions{})
			kind = EVENT
		case COMPONENT_STATUS:
			mgr.clientsets[cid].CoreV1().ComponentStatuses().Get(s.Name, metav1.GetOptions{})
			kind = COMPONENT_STATUS
		case NODE:
			mgr.clientsets[cid].CoreV1().Nodes().Get(s.Name, metav1.GetOptions{})
			kind = NODE
		case LIMIT_RANGE:
			mgr.clientsets[cid].CoreV1().LimitRanges(ns).Get(s.Name, metav1.GetOptions{})
			kind = LIMIT_RANGE
		case PERSISTENT_VOLUME_CLAIM:
			mgr.clientsets[cid].CoreV1().PersistentVolumeClaims(ns).Get(s.Name, metav1.GetOptions{})
			kind = PERSISTENT_VOLUME_CLAIM
		case PERSISTENT_VOLUME:
			mgr.clientsets[cid].CoreV1().PersistentVolumes().Get(s.Name, metav1.GetOptions{})
			kind = PERSISTENT_VOLUME
		case POD_TEMPLATE:
			mgr.clientsets[cid].CoreV1().PodTemplates(ns).Get(s.Name, metav1.GetOptions{})
			kind = POD_TEMPLATE
		case RESOURCE_QUOTA:
			mgr.clientsets[cid].CoreV1().ResourceQuotas(ns).Get(s.Name, metav1.GetOptions{})
			kind = RESOURCE_QUOTA
		case SECRET:
			mgr.clientsets[cid].CoreV1().Secrets(ns).Get(s.Name, metav1.GetOptions{})
			kind = SECRET
		case SERVICE_ACCOUNT:
			mgr.clientsets[cid].CoreV1().ServiceAccounts(ns).Get(s.Name, metav1.GetOptions{})
			kind = SERVICE_ACCOUNT
		case ROLE:
			mgr.clientsets[cid].RbacV1().Roles(ns).Get(s.Name, metav1.GetOptions{})
			kind = ROLE
		case ROLE_BINDING:
			mgr.clientsets[cid].RbacV1().RoleBindings(ns).Get(s.Name, metav1.GetOptions{})
			kind = ROLE_BINDING
		case CLUSTER_ROLE:
			mgr.clientsets[cid].RbacV1().ClusterRoles().Get(s.Name, metav1.GetOptions{})
			kind = CLUSTER_ROLE
		case CLUSTER_ROLE_BINDING:
			mgr.clientsets[cid].RbacV1().ClusterRoleBindings().Get(s.Name, metav1.GetOptions{})
			kind = CLUSTER_ROLE_BINDING
		}

		latency = metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)
		log.Infof("Get %s %v", kind, s.Name)

		mgr.alMutex.Lock()
		if mgr.apiTimes[kind] == nil {
			mgr.apiTimes[kind] = make(map[string][]time.Duration, 0)
		}
		// Update latency stats
		mgr.apiTimes[kind][LIST_ACTION] = append(mgr.apiTimes[kind][LIST_ACTION], latency)
		mgr.alMutex.Unlock()
	}
	return nil
}

/*
 * This function implements the DELETE action.
 */
func (mgr *ResourceManager) Delete(n interface{}) error {
	var kind string

	switch s := n.(type) {
	default:
		log.Errorf("Invalid spec %T for resource delete action.", s)
		return fmt.Errorf("Invalid spec %T for resource delete action.", s)
	case ActionSpec:
		cid := s.Tid % len(mgr.clientsets)
		/*ns := mgr.namespace
		if space, ok := mgr.resNs[s.Name]; ok {
			ns = space
		}*/

		options := GetListOptions(s)

		var resNames []string
		var latencies []time.Duration

		switch s.Kind {
		default:
			log.Errorf("Invalid kind %T for resource delete action.", s.Kind)
		case POD:
			resList, _ := mgr.clientsets[cid].CoreV1().Pods("").List(options)
			for _, item := range resList.Items {
				log.Infof("Deleting pod %v", item.Name)
				startTime := metav1.Now()
				ns := mgr.resNs[item.Name]
				mgr.clientsets[cid].CoreV1().Pods(ns).Delete(item.Name, nil)
				latencies = append(latencies, metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond))
				resNames = append(resNames, item.Name)
			}
			kind = POD
		case CONFIG_MAP:
			resList, _ := mgr.clientsets[cid].CoreV1().ConfigMaps("").List(options)
			for _, item := range resList.Items {
				log.Infof("Deleting configmap %v", item.Name)
				startTime := metav1.Now()
				ns := mgr.resNs[item.Name]
				mgr.clientsets[cid].CoreV1().ConfigMaps(ns).Delete(item.Name, nil)
				latencies = append(latencies, metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond))
				resNames = append(resNames, item.Name)
			}
			kind = CONFIG_MAP
		case ENDPOINTS:
			resList, _ := mgr.clientsets[cid].CoreV1().Endpoints("").List(options)
			for _, item := range resList.Items {
				log.Infof("Deleting endpoint %v", item.Name)
				startTime := metav1.Now()
				ns := mgr.resNs[item.Name]
				mgr.clientsets[cid].CoreV1().Endpoints(ns).Delete(item.Name, nil)
				latencies = append(latencies, metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond))
				resNames = append(resNames, item.Name)
			}
			kind = ENDPOINTS
		case EVENT:
			resList, _ := mgr.clientsets[cid].CoreV1().Events("").List(options)
			for _, item := range resList.Items {
				log.Infof("Deleting event %v", item.Name)
				startTime := metav1.Now()
				ns := mgr.resNs[item.Name]
				mgr.clientsets[cid].CoreV1().Events(ns).Delete(item.Name, nil)
				latencies = append(latencies, metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond))
				resNames = append(resNames, item.Name)
			}
			kind = EVENT
		case COMPONENT_STATUS:
			log.Errorf("Delete action is not supported on %s.", COMPONENT_STATUS)
			return fmt.Errorf("delete action not supported on the resource")
		case NODE:
			resList, _ := mgr.clientsets[cid].CoreV1().Nodes().List(options)
			for _, item := range resList.Items {
				log.Infof("Deleting node %v", item.Name)
				startTime := metav1.Now()
				mgr.clientsets[cid].CoreV1().Nodes().Delete(item.Name, nil)
				latencies = append(latencies, metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond))
				resNames = append(resNames, item.Name)
			}
			kind = NODE
		case LIMIT_RANGE:
			resList, _ := mgr.clientsets[cid].CoreV1().LimitRanges("").List(options)
			for _, item := range resList.Items {
				log.Infof("Deleting limit range %v", item.Name)
				startTime := metav1.Now()
				ns := mgr.resNs[item.Name]
				mgr.clientsets[cid].CoreV1().LimitRanges(ns).Delete(item.Name, nil)
				latencies = append(latencies, metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond))
				resNames = append(resNames, item.Name)
			}
			kind = LIMIT_RANGE
		case PERSISTENT_VOLUME_CLAIM:
			resList, _ := mgr.clientsets[cid].CoreV1().PersistentVolumeClaims("").List(options)
			for _, item := range resList.Items {
				log.Infof("Deleting persistent volume claim %v", item.Name)
				startTime := metav1.Now()
				ns := mgr.resNs[item.Name]
				mgr.clientsets[cid].CoreV1().PersistentVolumeClaims(ns).Delete(item.Name, nil)
				latencies = append(latencies, metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond))
				resNames = append(resNames, item.Name)
			}
			kind = PERSISTENT_VOLUME_CLAIM
		case PERSISTENT_VOLUME:
			resList, _ := mgr.clientsets[cid].CoreV1().PersistentVolumes().List(options)
			for _, item := range resList.Items {
				log.Infof("Deleting persistent volume %v", item.Name)
				startTime := metav1.Now()
				mgr.clientsets[cid].CoreV1().PersistentVolumes().Delete(item.Name, nil)
				latencies = append(latencies, metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond))
				resNames = append(resNames, item.Name)
			}
			kind = PERSISTENT_VOLUME
		case POD_TEMPLATE:
			resList, _ := mgr.clientsets[cid].CoreV1().PodTemplates("").List(options)
			for _, item := range resList.Items {
				log.Infof("Deleting pod template %v", item.Name)
				startTime := metav1.Now()
				ns := mgr.resNs[item.Name]
				mgr.clientsets[cid].CoreV1().PodTemplates(ns).Delete(item.Name, nil)
				latencies = append(latencies, metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond))
				resNames = append(resNames, item.Name)
			}
			kind = POD_TEMPLATE
		case RESOURCE_QUOTA:
			resList, _ := mgr.clientsets[cid].CoreV1().ResourceQuotas("").List(options)
			for _, item := range resList.Items {
				log.Infof("Deleting resource quota %v", item.Name)
				startTime := metav1.Now()
				ns := mgr.resNs[item.Name]
				mgr.clientsets[cid].CoreV1().ResourceQuotas(ns).Delete(item.Name, nil)
				latencies = append(latencies, metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond))
				resNames = append(resNames, item.Name)
			}
			kind = RESOURCE_QUOTA
		case SECRET:
			resList, _ := mgr.clientsets[cid].CoreV1().Secrets("").List(options)
			for _, item := range resList.Items {
				log.Infof("Deleting secret %v", item.Name)
				startTime := metav1.Now()
				ns := mgr.resNs[item.Name]
				mgr.clientsets[cid].CoreV1().Secrets(ns).Delete(item.Name, nil)
				latencies = append(latencies, metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond))
				resNames = append(resNames, item.Name)
			}
			kind = SECRET
		case SERVICE_ACCOUNT:
			resList, _ := mgr.clientsets[cid].CoreV1().ServiceAccounts("").List(options)
			for _, item := range resList.Items {
				log.Infof("Deleting service account %v", item.Name)
				startTime := metav1.Now()
				ns := mgr.resNs[item.Name]
				mgr.clientsets[cid].CoreV1().ServiceAccounts(ns).Delete(item.Name, nil)
				latencies = append(latencies, metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond))
				resNames = append(resNames, item.Name)
			}
			kind = SERVICE_ACCOUNT
		case ROLE:
			resList, _ := mgr.clientsets[cid].RbacV1().Roles("").List(options)
			for _, item := range resList.Items {
				log.Infof("Deleting role %v", item.Name)
				startTime := metav1.Now()
				ns := mgr.resNs[item.Name]
				mgr.clientsets[cid].RbacV1().Roles(ns).Delete(item.Name, nil)
				latencies = append(latencies, metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond))
				resNames = append(resNames, item.Name)
			}
			kind = ROLE
		case ROLE_BINDING:
			resList, _ := mgr.clientsets[cid].RbacV1().RoleBindings("").List(options)
			for _, item := range resList.Items {
				log.Infof("Deleting role binding %v", item.Name)
				startTime := metav1.Now()
				ns := mgr.resNs[item.Name]
				mgr.clientsets[cid].RbacV1().RoleBindings(ns).Delete(item.Name, nil)
				latencies = append(latencies, metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond))
				resNames = append(resNames, item.Name)
			}
			kind = ROLE_BINDING
		case CLUSTER_ROLE:
			resList, _ := mgr.clientsets[cid].RbacV1().ClusterRoles().List(options)
			for _, item := range resList.Items {
				log.Infof("Deleting cluster role %v", item.Name)
				startTime := metav1.Now()
				mgr.clientsets[cid].RbacV1().ClusterRoles().Delete(item.Name, nil)
				latencies = append(latencies, metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond))
				resNames = append(resNames, item.Name)
			}
			kind = CLUSTER_ROLE
		case CLUSTER_ROLE_BINDING:
			resList, _ := mgr.clientsets[cid].RbacV1().ClusterRoleBindings().List(options)
			for _, item := range resList.Items {
				log.Infof("Deleting cluster role binding %v", item.Name)
				startTime := metav1.Now()
				mgr.clientsets[cid].RbacV1().ClusterRoleBindings().Delete(item.Name, nil)
				latencies = append(latencies, metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond))
				resNames = append(resNames, item.Name)
			}
			kind = CLUSTER_ROLE_BINDING
		}

		mgr.alMutex.Lock()
		if mgr.apiTimes[kind] == nil {
			mgr.apiTimes[kind] = make(map[string][]time.Duration, 0)
		}
		// Update latency stats
		for _, l := range latencies {
			mgr.apiTimes[kind][DELETE_ACTION] = append(mgr.apiTimes[kind][DELETE_ACTION], l)
		}
		mgr.alMutex.Unlock()

		mgr.resMutex.Lock()
		// Delete it from the resource set
		for _, n := range resNames {
			_, ok := mgr.resNs[n]
			if ok {
				delete(mgr.resNs, n)
			}
		}
		mgr.resMutex.Unlock()

	}
	return nil
}

/*
 * This function implements the DeleteAll manager interface. It is used to clean
 * all the resources that are created by this manager.
 */
func (mgr *ResourceManager) DeleteAll() error {
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

	return nil
}

/*
 * This function computes all the metrics and stores the results into the log file.
 */
func (mgr *ResourceManager) LogStats() {

	log.Infof("----------------------------- Resource API Call Latencies (ms) " +
		"-----------------------------")
	log.Infof("%-50v %-10v %-10v %-10v %-10v", " ", "median", "min", "max", "99%")
	for k, _ := range mgr.apiTimes {
		for m, _ := range mgr.apiTimes[k] {
			sort.Slice(mgr.apiTimes[k][m],
				func(i, j int) bool { return mgr.apiTimes[k][m][i] < mgr.apiTimes[k][m][j] })
			mid := float32(mgr.apiTimes[k][m][len(mgr.apiTimes[k][m])/2]) / float32(time.Millisecond)
			min := float32(mgr.apiTimes[k][m][0]) / float32(time.Millisecond)
			max := float32(mgr.apiTimes[k][m][len(mgr.apiTimes[k][m])-1]) / float32(time.Millisecond)
			p99 := float32(mgr.apiTimes[k][m][len(mgr.apiTimes[k][m])-1-len(mgr.apiTimes[k][m])/100]) /
				float32(time.Millisecond)
			log.Infof("%-50v %-10v %-10v %-10v %-10v", m+" "+k+" latency: ",
				mid, min, max, p99)
		}
	}
}

func (mgr *ResourceManager) GetResourceName(opNum int, tid int, kind string) string {
	return benchPrefix + strings.ToLower(kind) + "-oid-" + strconv.Itoa(opNum) + "-tid-" + strconv.Itoa(tid)
}

func (mgr *ResourceManager) SendMetricToWavefront(now time.Time, wfTags []perf_util.WavefrontTag, wavefrontPathDir string, prefix string) {
	var points []perf_util.WavefrontDataPoint
	for k, _ := range mgr.apiTimes {
		for m, _ := range mgr.apiTimes[k] {
			mid := float32(mgr.apiTimes[k][m][len(mgr.apiTimes[k][m])/2]) / float32(time.Millisecond)
			min := float32(mgr.apiTimes[k][m][0]) / float32(time.Millisecond)
			max := float32(mgr.apiTimes[k][m][len(mgr.apiTimes[k][m])-1]) / float32(time.Millisecond)
			p99 := float32(mgr.apiTimes[k][m][len(mgr.apiTimes[k][m])-1-len(mgr.apiTimes[k][m])/100]) /
				float32(time.Millisecond)
			points = append(points, perf_util.WavefrontDataPoint{"resource.apicall." + m + ".median.latency",
				mid, now, mgr.source, wfTags})
			points = append(points, perf_util.WavefrontDataPoint{"resource.apicall." + m + ".min.latency",
				min, now, mgr.source, wfTags})
			points = append(points, perf_util.WavefrontDataPoint{"resource.apicall." + m + ".max.latency",
				max, now, mgr.source, wfTags})
			points = append(points, perf_util.WavefrontDataPoint{"resource.apicall." + m + ".p99.latency",
				p99, now, mgr.source, wfTags})

		}
	}

	var metricLines []string
	for _, point := range points {
		metricLines = append(metricLines, point.String())
		metricLines = append(metricLines, "\n")
	}

	perf_util.WriteDataPoints(now, points, wavefrontPathDir, prefix)
}

func (mgr *ResourceManager) CalculateStats() {
	// This manager has nothing to calculate
}

func (mgr *ResourceManager) CalculateSuccessRate() int {
	// This manager has nothing to calculate
	return 0
}
