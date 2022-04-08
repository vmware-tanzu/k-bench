package manager

import (
	"fmt"
    "context"
	"time"
	"sync"
	"strconv"
	"sort"
	"strings"
	log "github.com/sirupsen/logrus"
    "github.com/vmware-tanzu/vm-operator-api/api/v1alpha1"
	restclient "k8s.io/client-go/rest"
	apiv1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
    ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"
	"k8s.io/client-go/dynamic"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k-bench/perf_util"
)

const vmNamePrefix string = "kbench-vm-"

/*
 * vmManager manages vms actions and stats.
 */
type VmManager struct {
	client            ctrlClient.Client
	clientsets        []ctrlClient.Client
	namespaceClient   *kubernetes.Clientset

	// Mark the client side time stamp for virtual Machines
	startTimes        map[string]metav1.Time     // start creating Virtual Machine 
	createdTimes      map[string]metav1.Time   // virtual machine created and powered on.
	gotIpTimes        map[string]metav1.Time    // virtual machine gets an IP address
	ReadyTimes        map[string]metav1.Time     // virtual machine in ready state

	// A map to track the API response time for the supported actions
	apiTimes          map[string][]time.Duration

	namespace       string // The benchmark's default namespace for vm
	source          string
	config            *restclient.Config

	vmNs              map[string]string // Used to track vms to namespaces mappings
	nsSet             map[string]bool   // Used to track created non-default namespaces

	statsMutex        sync.RWMutex
	vmMutex           sync.Mutex
	alMutex           sync.Mutex

	// Action functions
	ActionFuncs       map[string]func(*VmManager, interface{}) error

	vmController      cache.Controller
	vmChan            chan struct{}
	vmThroughput      float32
	vmAvgLatency      float32
	negRes            bool 

	startTimestamp    string
	Wg                sync.WaitGroup

	startToCreatedLatency, createdToGotIpLatency, startToReadyLatency   perf_util.OperationLatencyMetric
}

func NewVmManager() Manager {
	cst := make(map[string]metav1.Time, 0)
	ct :=  make(map[string]metav1.Time, 0)
	git := make(map[string]metav1.Time, 0)
	rt :=  make(map[string]metav1.Time, 0)

	apt := make(map[string][]time.Duration, 0)

	vmn := make(map[string]string, 0)
	ns := make(map[string]bool, 0)
	af := make(map[string]func(*VmManager, interface{}) error, 0)

	af[CREATE_ACTION] = (*VmManager).Create
	// af[DELETE_ACTION] = (*VmManager).Delete
	// af[LIST_ACTION] = (*VmManager).List

	vmc := make(chan struct{})

	return &VmManager{
		startTimes:	    cst,
		createdTimes:	ct,
		gotIpTimes:		git,
		ReadyTimes:		rt,

		apiTimes: apt,

		namespace: "default", //apiv1.NamespaceDefault,
		vmNs:     vmn,
		nsSet:     ns,

		statsMutex: sync.RWMutex{},
		vmMutex:    sync.Mutex{},
		alMutex:    sync.Mutex{},

		ActionFuncs: af,
		//vmController: nil,
		vmChan:      vmc,
		startTimestamp: metav1.Now().Format("2006-01-02T15-04-05"),
	}
}

// This function adds cache with watch list and event handler
func (mgr *VmManager) initCache(resourceType string) {
	dynamicClient, err := dynamic.NewForConfig(mgr.config)
	if err != nil {
        panic(err)
    }
	var customeResource = schema.GroupVersionResource{Group: "vmoperator.vmware.com", Version: "v1alpha1", Resource: "virtualmachines"}
	dynInformer := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicClient, 0, mgr.namespace , nil)
	informer := dynInformer.ForResource(customeResource).Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			mgr.statsMutex.Lock()
			defer mgr.statsMutex.Unlock()
			u := obj.(*unstructured.Unstructured)
			unstructured := u.UnstructuredContent()
	 		var myvirtualmachine v1alpha1.VirtualMachine
	 		err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructured, &myvirtualmachine)

			if  myvirtualmachine.Status.Phase == "" {
				if _, ok := mgr.startTimes[myvirtualmachine.GetName()]; !ok {
					mgr.startTimes[myvirtualmachine.GetName()] = metav1.Now()
				}
				log.Info("Virtual Machine start creating...")
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

			if myvirtualmachine.Status.VmIp != "" &&  strings.Count(myvirtualmachine.Status.VmIp, ".") == 3 {
				if _, ok := mgr.gotIpTimes[myvirtualmachine.GetName()]; !ok {
					mgr.gotIpTimes[myvirtualmachine.GetName()] = metav1.Now()
				}
				if _, ok := mgr.ReadyTimes[myvirtualmachine.GetName()]; !ok {
					mgr.ReadyTimes[myvirtualmachine.GetName()] = metav1.Now()
				}
				log.Info("Virtual Machine got IP Address")
				
			} else if myvirtualmachine.Status.PowerState == v1alpha1.VirtualMachinePoweredOn || myvirtualmachine.Status.Phase == v1alpha1.Created {
				if _, ok := mgr.createdTimes[myvirtualmachine.GetName()]; !ok {
					mgr.createdTimes[myvirtualmachine.GetName()] = metav1.Now()
				}
				log.Info("Virtual Machine Created")
			}
		},
	})
	//mgr.vmController = &controller
	//go mgr.vmController.Run(mgr.vmChan)
	//go informer.Run(mgr.vmChan)
	go func() {
		stopper := make(chan struct{})
		defer close(stopper)
		informer.Run(stopper)
	}()
}

/*
 * This function implements the Init interface and is used to initialize the manager
 */
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
	namespaceClient, err := kubernetes.NewForConfig(kubeConfig)
	mgr.namespaceClient = namespaceClient
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
		nsSpec := &apiv1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
		_, err := mgr.namespaceClient.CoreV1().Namespaces().Create(context.Background(), nsSpec, metav1.CreateOptions{})
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
func (mgr *VmManager) Create(spec interface{}) error {
	vmspec := spec.(*v1alpha1.VirtualMachine)
	switch s := spec.(type) {
	default:
		//log.Errorf("Invalid spec type %T for vm create action.", s)
		return fmt.Errorf("Invalid spec type %T for Vm create action.", s)
	case *v1alpha1.VirtualMachine:
		tid, _ := strconv.Atoi(s.Labels["tid"])
		cid := tid % len(mgr.clientsets)

		ns := mgr.namespace
		if s.Namespace != "" {
			ns = s.Namespace
			mgr.vmMutex.Lock()
			if _, exist := mgr.nsSet[ns]; !exist && ns != "default" {
				nsSpec := &apiv1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
				_, err := mgr.namespaceClient.CoreV1().Namespaces().Create(context.Background(), nsSpec, metav1.CreateOptions{})
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
			mgr.vmMutex.Unlock()
		}

		startTime := metav1.Now()
		err := mgr.clientsets[cid].Create(context.TODO(), vmspec)
		latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

		if err != nil {
			fmt.Printf("Error ",err)
			panic(err)
		}

		mgr.alMutex.Lock()
		mgr.apiTimes[CREATE_ACTION] = append(mgr.apiTimes[CREATE_ACTION], latency)
		mgr.alMutex.Unlock()

		mgr.vmMutex.Lock()
		fmt.Printf("Virtual Machine name %v", vmspec.GetName())
		mgr.vmNs[vmspec.GetName()] = ns
		mgr.vmMutex.Unlock()
	}
	return nil
}

/*
 * This function implements the LIST action.
 */
func (mgr *VmManager) List(n interface{}) error {
	log.Info("Nedd to implement this later!!....")
	return nil
}

/*
 * This function implements the DELETE action.
 */
func (mgr *VmManager) Delete(n interface{}) error {
	log.Info("Nedd to implement this later!!....")
	return nil
}

/*
 * This function implements the DeleteAll manager interface. It is used to clean
 * all the resources that are created by the vm manager.
 */
func (mgr *VmManager) DeleteAll() error {
	log.Info("Nedd to implement this later!!....")
	return nil
}

/*
 * This function returns whether all the created vms become ready
 */
func (mgr *VmManager) IsStable() bool {
	return len(mgr.ReadyTimes) == len(mgr.apiTimes[CREATE_ACTION])
}

/*
 * This function computes all the metrics and stores the results into the log file.
 */
func (mgr *VmManager) LogStats() {
	log.Infof("------------------------------------ VM Operation Summary " +
		"-----------------------------------")
	log.Infof("%-50v %-10v", "Number of valid vm requests:",
		len(mgr.apiTimes[CREATE_ACTION]))
	log.Infof("%-50v %-10v", "Number of create vm:", len(mgr.startTimes))
	log.Infof("%-50v %-10v", "Number of vms created:", len(mgr.createdTimes))
	log.Infof("%-50v %-10v", "Number of vms got IP:", len(mgr.gotIpTimes))
	log.Infof("%-50v %-10v", "Number of vms ready:", len(mgr.ReadyTimes))

	log.Infof("%-50v %-10v", "Vm creation throughput (vms/minutes):",
		mgr.vmThroughput)
	log.Infof("%-50v %-10v", "Vm creation average latency:",
		mgr.vmAvgLatency)

	log.Infof("--------------------------------- Vm Startup Latencies (ms) " +
		"---------------------------------")
	log.Infof("%-50v %-10v %-10v %-10v %-10v", " ", "median", "min", "max", "99%")

	var latency perf_util.OperationLatencyMetric
	latency = mgr.startToCreatedLatency
	if latency.Valid {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Vm creation latency stats (client): ",
			latency.Latency.Mid, latency.Latency.Min, latency.Latency.Max, latency.Latency.P99)
	} else {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Vm creation latency stats (client): ",
			"---", "---", "---", "---")
	}

	latency = mgr.createdToGotIpLatency
	if latency.Valid {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Vm Got IP latency stats (client): ",
			latency.Latency.Mid, latency.Latency.Min, latency.Latency.Max, latency.Latency.P99)
	} else {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Vm Got IP latency stats (client): ",
			"---", "---", "---", "---")
	}

	latency = mgr.startToReadyLatency
	if latency.Valid {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Vm client e2e latency: ",
			latency.Latency.Mid, latency.Latency.Min, latency.Latency.Max, latency.Latency.P99)
	} else {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Vm client e2e latency (create-to-ready): ",
			"---", "---", "---", "---")
	}

	log.Infof("--------------------------------- Vm API Call Latencies (ms) " +
		"--------------------------------")
	log.Infof("%-50v %-10v %-10v %-10v %-10v", " ", "median", "min", "max", "99%")

	var mid, min, max, p99 float32
	for m, _ := range mgr.apiTimes {
		mid = float32(mgr.apiTimes[m][len(mgr.apiTimes[m])/2]) / float32(time.Millisecond)
		min = float32(mgr.apiTimes[m][0]) / float32(time.Millisecond)
		max = float32(mgr.apiTimes[m][len(mgr.apiTimes[m])-1]) / float32(time.Millisecond)
		p99 = float32(mgr.apiTimes[m][len(mgr.apiTimes[m])-1-len(mgr.apiTimes[m])/100]) /
			float32(time.Millisecond)
		log.Infof("%-50v %-10v %-10v %-10v %-10v", m+" Vm latency: ", mid, min, max, p99)
	}

	// if mgr.createdToGotIpLatency.Latency.Mid < 0 {
	// 	log.Warning("There might be time skew between server and nodes, " +
	// 		"server side metrics such as scheduling latency stats (server) above is negative.")
	// }

	// If we see negative server side results or server-client latency is larger than client latency by more than 3x
	// if mgr.negRes || mgr.startToReadyLatency.Latency.Mid/3 > mgr.firstToReadyLatency.Latency.Mid {
	// 	log.Warning("There might be time skew between client and server, " +
	// 		"and certain results (e.g., client-server e2e latency) above " +
	// 		"may have been affected.")
	// }

}

func (mgr *VmManager) GetResourceName(userVmPrefix string, opNum int, tid int) string {
	if userVmPrefix == "" {
		return vmNamePrefix + "oid-" + strconv.Itoa(opNum) + "-tid-" + strconv.Itoa(tid)
	} else {
		return userVmPrefix + "-" + vmNamePrefix + "oid-" + strconv.Itoa(opNum) + "-tid-" + strconv.Itoa(tid)
	}
}

func (mgr *VmManager) SendMetricToWavefront(
	now time.Time,
	wfTags []perf_util.WavefrontTag,
	wavefrontPathDir string,
	prefix string) {

	log.Info("Current Not Supported For VM Operations!!...")

}

// Get op num given vm name
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
	latPerOp := make(map[int][]float32, 0)
	var totalLat float32
	totalLat = 0.0
	vmCount := 0
	// The below loop groups the latency by operation
	for p, ct := range mgr.ReadyTimes {
		opn := mgr.getOpNum(p)
		if opn == -1 {
			continue
		}
		nl := float32(ct.Time.Sub(mgr.startTimes[p].Time)) / float32(time.Second)
		latPerOp[opn] = append(latPerOp[opn], nl)
		totalLat += nl
		vmCount += 1
	}

	var accStartTime float32
	accStartTime = 0.0
	accvms := 0

	for opn, _ := range latPerOp {
		sort.Slice(latPerOp[opn],
			func(i, j int) bool { return latPerOp[opn][i] < latPerOp[opn][j] })

		curLen := len(latPerOp[opn])
		accStartTime += float32(latPerOp[opn][curLen/2])
		accvms += (curLen + 1) / 2
	}

	mgr.vmAvgLatency = totalLat / float32(vmCount)
	mgr.vmThroughput = float32(accvms) * float32(60) / accStartTime

	startToCreatedLatency := make([]time.Duration, 0)
	createdToGotIpLatency := make([]time.Duration, 0)
	startToReadyLatency := make([]time.Duration, 0)

	for p, ct := range mgr.startTimes {
		if st, ok := mgr.createdTimes[p]; ok {
			startToCreatedLatency = append(startToCreatedLatency, st.Time.Sub(ct.Time).
				Round(time.Microsecond))
		}
	}
	for p, ct := range mgr.createdTimes {
		if st, ok := mgr.gotIpTimes[p]; ok {
			createdToGotIpLatency = append(createdToGotIpLatency, st.Time.Sub(ct.Time).
				Round(time.Microsecond))
		}
	}
	for p, ct := range mgr.startTimes {
		if st, ok := mgr.ReadyTimes[p]; ok {
			startToReadyLatency = append(startToReadyLatency, st.Time.Sub(ct.Time).
				Round(time.Microsecond))
		}
	}

	sort.Slice(startToCreatedLatency,
		func(i, j int) bool { return startToCreatedLatency[i] < startToCreatedLatency[j] })
	sort.Slice(createdToGotIpLatency,
		func(i, j int) bool { return createdToGotIpLatency[i] < createdToGotIpLatency[j] })
	sort.Slice(startToReadyLatency,
		func(i, j int) bool { return startToReadyLatency[i] < startToReadyLatency[j] })
	
	var mid, min, max, p99 float32

	if len(startToCreatedLatency) > 0 {
		mid = float32(startToCreatedLatency[len(startToCreatedLatency)/2]) / float32(time.Millisecond)
		min = float32(startToCreatedLatency[0]) / float32(time.Millisecond)
		max = float32(startToCreatedLatency[len(startToCreatedLatency)-1]) / float32(time.Millisecond)
		p99 = float32(startToCreatedLatency[len(startToCreatedLatency)-1-len(startToCreatedLatency)/100]) /
			float32(time.Millisecond)
		mgr.startToCreatedLatency.Valid = true
		mgr.startToCreatedLatency.Latency = perf_util.LatencyMetric{mid, min, max, p99}
	}
	if len(createdToGotIpLatency) > 0 {
		mid = float32(createdToGotIpLatency[len(createdToGotIpLatency)/2]) / float32(time.Millisecond)
		min = float32(createdToGotIpLatency[0]) / float32(time.Millisecond)
		max = float32(createdToGotIpLatency[len(createdToGotIpLatency)-1]) / float32(time.Millisecond)
		p99 = float32(createdToGotIpLatency[len(createdToGotIpLatency)-1-len(createdToGotIpLatency)/100]) /
			float32(time.Millisecond)
		mgr.createdToGotIpLatency.Valid = true
		mgr.createdToGotIpLatency.Latency = perf_util.LatencyMetric{mid, min, max, p99}
	}
	if len(startToReadyLatency) > 0 {
		mid = float32(startToReadyLatency[len(startToReadyLatency)/2]) / float32(time.Millisecond)
		min = float32(startToReadyLatency[0]) / float32(time.Millisecond)
		max = float32(startToReadyLatency[len(startToReadyLatency)-1]) / float32(time.Millisecond)
		p99 = float32(startToReadyLatency[len(startToReadyLatency)-1-len(startToReadyLatency)/100]) /
			float32(time.Millisecond)
		mgr.startToReadyLatency.Valid = true
		mgr.startToReadyLatency.Latency = perf_util.LatencyMetric{mid, min, max, p99}
	}

	for m, _ := range mgr.apiTimes {
		sort.Slice(mgr.apiTimes[m],
			func(i, j int) bool { return mgr.apiTimes[m][i] < mgr.apiTimes[m][j] })
	}

}

func (mgr *VmManager) CalculateSuccessRate() int {
	if len(mgr.startTimes) == 0 {
		return 0
	}
	return len(mgr.ReadyTimes) * 100 / len(mgr.startTimes)
}