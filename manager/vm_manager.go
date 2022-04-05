package manager

import (
	"fmt"
    "context"
	"time"
	"sync"
	"strconv"
	"strings"
	//"path/filepath"
	log "github.com/sirupsen/logrus"
    "github.com/vmware-tanzu/vm-operator-api/api/v1alpha1"
	restclient "k8s.io/client-go/rest"
	//"os"
	//"flag"
	//"k8s.io/client-go/tools/clientcmd"
	//"k8s.io/client-go/util/homedir"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/runtime"
	//ktypes "k8s.io/apimachinery/pkg/types"
	//"k8s.io/apimachinery/pkg/util/wait"
    //"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	//"k8s.io/apimachinery/pkg/watch"
	//"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/apimachinery/pkg/runtime/schema"
    ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"
	"k8s.io/client-go/dynamic"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic/dynamicinformer"
	//"k8s.io/client-go/util/workqueue"
	//"os/signal"
	"k-bench/perf_util"
)

const vmNamePrefix string = "kbench-vm-"

// /*
//  * PodManager manages pods actions and stats.
//  */
type VmManager struct {
	client ctrlClient.Client
	clientsets []ctrlClient.Client
	
	// st map[string]metav1.Time
	// cst map[string]metav1.Time
	// ct  map[string]metav1.Time
	gt map[string]string
	vmThroughput float32
	vmAvgLatency float32 
	Wg sync.WaitGroup
	statsMutex sync.RWMutex

	// A map to track the API response time for the supported actions
	apiTimes map[string][]time.Duration

	namespace string // The benchmark's default namespace for pod
	source    string
	config    *restclient.Config

	// podNs map[string]string // Used to track pods to namespaces mappings
	// nsSet map[string]bool   // Used to track created non-default namespaces

	alMutex sync.Mutex

	// Action functions
	ActionFuncs map[string]func(*VmManager, interface{}) error

	vmChan       chan struct{}

	negRes        bool

	startTimestamp string
	podMutex sync.Mutex

	// createToScheLatency, scheToStartLatency   perf_util.OperationLatencyMetric
	// startToPulledLatency, pulledToRunLatency  perf_util.OperationLatencyMetric
	// createToRunLatency, firstToSchedLatency   perf_util.OperationLatencyMetric
	// schedToInitdLatency, initdToReadyLatency  perf_util.OperationLatencyMetric
	// firstToReadyLatency, createToReadyLatency perf_util.OperationLatencyMetric
}

func NewVmManager() Manager {
	//clientsets := make([]ctrlClient.Client, maxClients)
	// creationStartedTimes := make(map[string]metav1.Time, 0)
	// startTime := make(map[string]metav1.Time, 0)
	// createdTimes := make(map[string]metav1.Time, 0)
	// gotIpTimes := make(map[string]metav1.Time, 0)

	apt := make(map[string][]time.Duration, 0)

	// pn := make(map[string]metav1.Time, 0)
	// ns := make(map[string]bool, 0)
	af := make(map[string]func(*VmManager, interface{}) error, 0)

	af[CREATE_ACTION] = (*VmManager).Create
	//af[RUN_ACTION] = (*VmManager).Run
	//af[DELETE_ACTION] = (*VmManager).Delete
	//af[LIST_ACTION] = (*VmManager).List
	//af[GET_ACTION] = (*VmManager).Get
	//af[UPDATE_ACTION] = (*VmManager).Update
	//af[COPY_ACTION] = (*VmManager).Copy

	vc := make(chan struct{})

	return &VmManager{
		// creationStartedTimes: cst,
		// createdTimes: ct,
		// gotIpTimes: gt,
		// startTime: st,

		apiTimes: apt,

		namespace: "kbench-vm-namespace", //apiv1.NamespaceDefault,
		//podNs:     pn,
		//nsSet:     ns,

		statsMutex: sync.RWMutex{},
		podMutex:   sync.Mutex{},
		alMutex:    sync.Mutex{},

		ActionFuncs: af,
		//podController: nil,
		vmChan:      vc,
		//startTimestamp: metav1.Now().Format("2006-01-02T15-04-05"),
	}
}

// // This function checks the pod's status and updates various timestamps.

// // This function adds cache with watch list and event handler
func (mgr *VmManager) initCache(resourceType string) {
	dynamicClient, err := dynamic.NewForConfig(mgr.config)
	if err != nil {
        panic(err)
    }
	var customeResource = schema.GroupVersionResource{Group: "vmoperator.vmware.com", Version: "v1alpha1", Resource: "virtualmachines"}
	dynInformer := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicClient,0,"ns1",nil)
	informer := dynInformer.ForResource(customeResource).Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			mgr.statsMutex.Lock()
			defer mgr.statsMutex.Unlock()
			fmt.Printf("Virtual Machine Added to the cluster.........................\n")
			u := obj.(*unstructured.Unstructured)
			unstructured := u.UnstructuredContent()
	 		var myvirtualmachine v1alpha1.VirtualMachine
	 		err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructured, &myvirtualmachine)
			//fmt.Printf("my vm object is ",myvirtualmachine.Status.Phase)
			if  myvirtualmachine.Status.Phase == "" {
				//mgr.creationStartedTimes[myvirtualmachine.GetName()] = time.Now().Format("2006-01-02T15-04-05.999")
				log.Info("Virtual Machine Creation Started")
				fmt.Printf("Virtual Machine Creation Started !!...%s---..%s\n",myvirtualmachine.GetName(),time.Now().Format("2006-01-02T15-04-05.999"))
			}

		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			//fmt.Printf("\nUpdate func called\n")
			mgr.statsMutex.Lock()
			defer mgr.statsMutex.Unlock()
			u := newObj.(*unstructured.Unstructured)
			unstructured := u.UnstructuredContent()
			var myvirtualmachine v1alpha1.VirtualMachine
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructured, &myvirtualmachine)

			if myvirtualmachine.Status.VmIp != "" {
				//mgr.gotIpTimes[myvirtualmachine.GetName()] = time.Now().Format("2006-01-02T15-04-05.999")
				log.Info("Virtual Machine got IP Address")
				fmt.Printf("\nVm Machine %s got IP Address  %s --At time %s\n\n",myvirtualmachine.GetName(), myvirtualmachine.Status.VmIp,time.Now().Format("2006-01-02T15-04-05.999"))
				
			} else if myvirtualmachine.Status.PowerState == v1alpha1.VirtualMachinePoweredOn || myvirtualmachine.Status.Phase == v1alpha1.Created {
				//mgr.createdTimes[myvirtualmachine.GetName()] = time.Now().Format("2006-01-02T15-04-05.999")
				log.Info("Virtual Machine Created")
				fmt.Printf("\nVirtual Machine Created %s !!.....%s\n",myvirtualmachine.GetName(),time.Now().Format("2006-01-02T15-04-05.999"))
				fmt.Printf("\nHey Virtual Machine is powered ON %s !!.......%s\n",myvirtualmachine.GetName(),time.Now().Format("2006-01-02T15-04-05.999"))
			}
		},
	})
	//mgr.podController = &controller
	//go mgr.podController.Run(mgr.vmChan)
	//go informer.Run(mgr.vmChan)
	go func() {
		stopper := make(chan struct{})
		defer close(stopper)
		informer.Run(stopper)
	}()
}

// /*
//  * This function updates the stats before deletion
//  */
// func (mgr *VmManager) UpdateBeforeDeletion(name string, ns string) {
// 	// Before deletion, make sure schedule and pulled time retrieved for this pod
// 	// As deletes may happen in multi-threaded section, need to protect the update
// 	mgr.statsMutex.Lock()
// 	if _, ok := mgr.scheduleTimes[name]; !ok {
// 		selector := fields.Set{
// 			"involvedObject.kind":      "Pod",
// 			"involvedObject.namespace": ns,
// 			//"source":                   apiv1.DefaultSchedulerName,
// 		}.AsSelector().String()
// 		options := metav1.ListOptions{FieldSelector: selector}
// 		//TODO: move the below statement out side the lock?
// 		events, err := mgr.client.CoreV1().Events("").List(context.Background(), options)
// 		if err != nil {
// 			log.Error(err)
// 		} else {
// 			scheEvents := make(map[string]metav1.Time, 0)
// 			pulledEvents := make(map[string]metav1.Time, 0)
// 			for _, event := range events.Items {
// 				if event.Source.Component == apiv1.DefaultSchedulerName {
// 					scheEvents[event.InvolvedObject.Name] = event.FirstTimestamp
// 				} else if event.Reason == "Pulled" {
// 					pulledEvents[event.InvolvedObject.Name] = event.FirstTimestamp
// 				}
// 			}

// 			for k := range mgr.createTimes {
// 				if _, sche_exist := scheEvents[k]; sche_exist {
// 					mgr.scheduleTimes[k] = scheEvents[k]
// 				}
// 				if _, pull_exist := scheEvents[k]; pull_exist {
// 					mgr.pulledTimes[k] = pulledEvents[k]
// 				}
// 			}
// 		}
// 	}
// 	mgr.statsMutex.Unlock()
// }

// /*
//  * This function implements the Init interface and is used to initialize the manager
//  */
func (mgr *VmManager) Init(
	kubeConfig *restclient.Config,
	nsName string,
	createNamespace bool,
	maxClients int,
	resourceType string,
) {
	mgr.namespace = nsName
	//mgr.source = perf_util.GetHostnameFromUrl(kubeConfig.Host)
	mgr.config = kubeConfig
	scheme := runtime.NewScheme()
    _ = v1alpha1.AddToScheme(scheme)

	sharedClient, err := ctrlClient.New(kubeConfig, ctrlClient.Options{
		Scheme: scheme,
	})

	if err != nil {
		panic(err)
	}

	mgr.client = sharedClient

	mgr.clientsets = make([]ctrlClient.Client, maxClients)

	for i := 0; i < maxClients; i++ {
		client, ce := ctrlClient.New(kubeConfig, ctrlClient.Options{
			Scheme: scheme,
		})
		if ce != nil {
			panic(ce)
		}

		mgr.clientsets[i] = client
	}

	if createNamespace {
	}

	mgr.initCache(resourceType)

}

// /*
//  * This function implements the CREATE action.
//  */
func (mgr *VmManager) Create(spec interface{}) error {
	obj := spec.(*v1alpha1.VirtualMachine)
	switch s := spec.(type) {
	default:
		//log.Errorf("Invalid spec type %T for Pod create action.", s)
		return fmt.Errorf("Invalid spec type %T for Vm create action.", s)
	case *v1alpha1.VirtualMachine:
	tid, _ := strconv.Atoi(s.Labels["tid"])
	cid := tid % len(mgr.clientsets)

	//ns := mgr.namespace
	// newVM := v1alpha1.VirtualMachine{
	// 	TypeMeta: metav1.TypeMeta{},
	// 	ObjectMeta: metav1.ObjectMeta{
	// 		Name:       "crd",
	// 		Namespace:  "ns1",
	// 	},
	// 	Spec: v1alpha1.VirtualMachineSpec{
	// 		ClassName: "best-effort-small",
	// 		ImageName: "photon-3-k8s-v1.20.7---vmware.1-tkg.1.7fb9067", //"ubuntu-20-04-vmservice-v1alpha1-20210528-ovf",
	// 		StorageClass: "wcp-storage-policy",
	// 		PowerState: "poweredOn",
	// 	},
	// }
	startTime := metav1.Now()
	log.Info("Start Creating VM's in VM Manager!!........")
	err := mgr.clientsets[cid].Create(context.TODO(), obj)
	if err != nil {
		fmt.Printf("Error ",err)
		panic(err)
	}
	latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

	mgr.alMutex.Lock()
	mgr.apiTimes[CREATE_ACTION] = append(mgr.apiTimes[CREATE_ACTION], latency)
	mgr.alMutex.Unlock()

		// mgr.podMutex.Lock()
		// mgr.podNs[pod.Name] = ns
		// mgr.podMutex.Unlock()
	}
	return nil
}

// /*
//  * This function implements the LIST action.
//  */
// func (mgr *VmManager) List(n interface{}) error {

// 	// switch s := n.(type) {
// 	// default:
// 	// 	log.Errorf("Invalid spec type %T for Pod list action.", s)
// 	// 	return fmt.Errorf("Invalid spec type %T for Pod list action.", s)
// 	// case ActionSpec:
// 	options := GetListOptions(s)

// 	cid := s.Tid % len(mgr.clientsets)

// 	ns := mgr.namespace
// 	if s.Namespace != "" {
// 		ns = s.Namespace
// 	}

// 	startTime := metav1.Now()
// 	pods, err := mgr.clientsets[cid].List(context.Background(), options)
// 	latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

// 	if err != nil {
// 		return err
// 	}
// 	log.Infof("Listed %v pods", len(pods.Items))

// 	mgr.alMutex.Lock()
// 	mgr.apiTimes[LIST_ACTION] = append(mgr.apiTimes[LIST_ACTION], latency)
// 	mgr.alMutex.Unlock()
// 	//}
// 	return nil
// }

// /*
//  * This function implements the GET action.
//  */
// func (mgr *VmManager) Get(n interface{}) error {

// 	// switch s := n.(type) {
// 	// default:
// 	// 	log.Errorf("Invalid spec type %T for Pod get action.", s)
// 	// 	return fmt.Errorf("Invalid spec type %T for Pod get action.", s)
// 	// case ActionSpec:
// 	cid := s.Tid % len(mgr.clientsets)
// 	ns := mgr.namespace
// 	if s.Namespace != "" {
// 		ns = s.Namespace
// 	}
// 	// Labels (or other filters) are ignored as they do not make sense to GET
// 	startTime := metav1.Now()
// 	pod, err := mgr.clientsets[cid].Get(context.Background(), s.Name, metav1.GetOptions{})
// 	latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

// 	if err != nil {
// 		return err
// 	}

// 	log.Infof("Got pod %v", pod.Name)

// 	mgr.alMutex.Lock()
// 	mgr.apiTimes[GET_ACTION] = append(mgr.apiTimes[GET_ACTION], latency)
// 	mgr.alMutex.Unlock()
// 	//}
// 	return nil
// }

// /*
//  * This function implements the RUN action.
//  */

// /*
//  * This function implements the COPY action.
//  */


// /*
//  * This function implements the UPDATE action.
//  */

// /*
//  * This function implements the DELETE action.
//  */
func (mgr *VmManager) Delete(n interface{}) error {
	// switch s := n.(type) {
	// default:
	// 	log.Errorf("Invalid spec %T for Pod delete action.", s)
	// 	return fmt.Errorf("Invalid spec %T for Pod delete action.", s)
	// case ActionSpec:
	// cid := s.Tid % len(mgr.clientsets)

	// options := GetListOptions(s)

	// ns := mgr.namespace
	// /*if space, ok := mgr.podNs[s.Name]; ok {
	// 	ns = space
	// }*/

	// if s.Namespace != "" {
	// 	ns = s.Namespace
	// }

	// pods := make([]apiv1.Pod, 0)

	// podList, err := mgr.clientsets[cid].List(context.Background(), options)
	// if err != nil {
	// 	return err
	// }
	// pods = podList.Items

	// for _, currPod := range pods {
	// 	log.Infof("Deleting pod %v", currPod.Name)
	// 	if _, ok := mgr.scheduleTimes[currPod.Name]; !ok {
	// 		mgr.UpdateBeforeDeletion(currPod.Name, ns)
	// 	}

	// 	// Delete the pod
	// 	startTime := metav1.Now()
	// 	mgr.clientsets[cid].Delete(context.Background(), currPod.Name, metav1.DeleteOptions{})
	// 	latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

	// 	mgr.alMutex.Lock()
	// 	mgr.apiTimes[DELETE_ACTION] = append(mgr.apiTimes[DELETE_ACTION], latency)
	// 	mgr.alMutex.Unlock()

	// 	mgr.podMutex.Lock()
	// 	// Delete it from the pod set
	// 	_, ok := mgr.podNs[currPod.Name]

	// 	if ok {
	// 		delete(mgr.podNs, currPod.Name)
	// 	}
	// 	mgr.podMutex.Unlock()
	// }
	//}
	return nil
}

// /*
//  * This function implements the DeleteAll manager interface. It is used to clean
//  * all the resources that are created by the pod manager.
//  */
func (mgr *VmManager) DeleteAll() error {
	// if len(mgr.podNs) > 0 {
	// 	log.Infof("Deleting all pods created by the pod manager...")
	// 	for name, _ := range mgr.podNs {
	// 		// Just use tid 0 so that the first client is used to delete all pods
	// 		mgr.Delete(ActionSpec{
	// 			Name: name,
	// 			Tid:  0})
	// 	}
	// 	mgr.podNs = make(map[string]string, 0)
	// } else {
	// 	log.Infof("Found no pod to delete, maybe they have already been deleted.")
	// }

	// if mgr.namespace != apiv1.NamespaceDefault {
	// 	mgr.clientsets[cid].Delete(context.Background(), mgr.namespace, metav1.DeleteOptions{})
	// }

	// // Delete other non default namespaces
	// for ns, _ := range mgr.nsSet {
	// 	if ns != apiv1.NamespaceDefault {
	// 		mgr.clientsets[cid].Delete(context.Background(), ns, metav1.DeleteOptions{})
	// 	}
	// }
	// mgr.nsSet = make(map[string]bool, 0)

	// close(mgr.vmChan)
	return nil
}

// /*
//  * This function returns whether all the created pods become ready
//  */
func (mgr *VmManager) IsStable() bool {
	return false//len(mgr.cReadyTimes) == len(mgr.apiTimes[CREATE_ACTION])
}

// /*
//  * This function computes all the metrics and stores the results into the log file.
//  */
func (mgr *VmManager) GetResourceName(userVmPrefix string, opNum int, tid int) string {
	if userVmPrefix == "" {
		return vmNamePrefix + "oid-" + strconv.Itoa(opNum) + "-tid-" + strconv.Itoa(tid)
	} else {
		return userVmPrefix + "-" + vmNamePrefix + "oid-" + strconv.Itoa(opNum) + "-tid-" + strconv.Itoa(tid)
	}
}

// // Get op num given pod name
func (mgr *VmManager) getOpNum(name string) int {
	//start := len(vmNamePrefix)
	start := strings.LastIndex(name, "-oid-") + len("-oid-")
	end := strings.LastIndex(name, "-tid-")

	opStr := name[start:end]
	res, err := strconv.Atoi(opStr)
	if err != nil {
		return -1
	}
	return res
}

func (mgr *VmManager) CalculateStats() {
	// The below loop groups the latency by operation
}

func (mgr *VmManager) CalculateSuccessRate() int {
	// if len(mgr.cFirstTimes) == 0 {
	// 	return 0
	// }
	// return len(mgr.cReadyTimes) * 100 / len(mgr.cFirstTimes)
	return 0
}

func (mgr *VmManager) LogStats() {
	// log.Infof("------------------------------------ Pod Operation Summary " +
	// 	"-----------------------------------")
}

func (mgr *VmManager) SendMetricToWavefront(
	now time.Time,
	wfTags []perf_util.WavefrontTag,
	wavefrontPathDir string,
	prefix string) {

	}