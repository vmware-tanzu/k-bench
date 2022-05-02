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
    "gitlab.eng.vmware.com/core-build/guest-cluster-controller/apis/run.tanzu/v1alpha1"
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

const tkgNamePrefix string = "kbench-tkg-"

/*
 * tkgManager manages tkgs actions and stats.
 */
type TkgManager struct {
	client            ctrlClient.Client
	clientsets        []ctrlClient.Client
	namespaceClient   *kubernetes.Clientset

	// Mark the client side time stamp for virtual Machines
	startTimes        map[string]metav1.Time     // start creating Virtual Machine 
	readyTimes      map[string]metav1.Time     // virtual machine in ready state

	// A map to track the API response time for the supported actions
	apiTimes          map[string][]time.Duration

	namespace       string // The benchmark's default namespace for tkg
	source          string
	config          *restclient.Config

	tkgNs             map[string]string // Used to track tkgs to namespaces mappings
	nsSet             map[string]bool   // Used to track created non-default namespaces

	statsMutex        sync.RWMutex
	tkgMutex          sync.Mutex
	alMutex           sync.Mutex

	// Action functions
	ActionFuncs       map[string]func(*TkgManager, interface{}) error

	tkgController      cache.Controller
	tkgChan            chan struct{}
	tkgThroughput      float32
	tkgAvgLatency      float32
	negRes            bool 

	startTimestamp    string
	Wg                sync.WaitGroup

	startToReadyLatency   perf_util.OperationLatencyMetric
}

func NewTkgManager() Manager {
	cst := make(map[string]metav1.Time, 0)
	crt := make(map[string]metav1.Time, 0)

	apt := make(map[string][]time.Duration, 0)

	tkgn := make(map[string]string, 0)
	ns := make(map[string]bool, 0)
	af := make(map[string]func(*TkgManager, interface{}) error, 0)

	af[CREATE_ACTION] = (*TkgManager).Create
	// af[DELETE_ACTION] = (*TkgManager).Delete
	// af[LIST_ACTION] = (*TkgManager).List

	tkgc := make(chan struct{})

	return &TkgManager{
		startTimes:	    cst,
		readyTimes:	crt,

		apiTimes: apt,

		namespace: "default", //apiv1.NamespaceDefault,
		tkgNs:     tkgn,
		nsSet:     ns,

		statsMutex: sync.RWMutex{},
		tkgMutex:   sync.Mutex{},
		alMutex:    sync.Mutex{},

		ActionFuncs: af,
		//tkgController: nil,
		tkgChan:      tkgc,
		startTimestamp: metav1.Now().Format("2006-01-02T15-04-05"),
	}
}

// This function adds cache with watch list and event handler
func (mgr *TkgManager) initCache(resourceType string) {
	dynamicClient, err := dynamic.NewForConfig(mgr.config)
	if err != nil {
        panic(err)
    }
	var customeResource = schema.GroupVersionResource{Group: "run.tanzu.vmware.com", Version: "v1alpha1", Resource: "tanzukubernetesclusters"}
	dynInformer := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicClient, 0, mgr.namespace , nil)
	informer := dynInformer.ForResource(customeResource).Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			mgr.statsMutex.Lock()
			defer mgr.statsMutex.Unlock()
			u := obj.(*unstructured.Unstructured)
			unstructured := u.UnstructuredContent()
	 		var mytkg_cluster v1alpha1.TanzuKubernetesCluster
	 		err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructured, &mytkg_cluster)

			if  mytkg_cluster.Status.Phase == "" {
				log.Info("TKC Added to the cluster...")
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			//fmt.Printf("\nUpdate func called\n")
			mgr.statsMutex.Lock()
			defer mgr.statsMutex.Unlock()
			u := newObj.(*unstructured.Unstructured)
			unstructured := u.UnstructuredContent()
			var mytkg_cluster v1alpha1.TanzuKubernetesCluster
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructured, &mytkg_cluster)

			if mytkg_cluster.Status.Phase == v1alpha1.TanzuKubernetesClusterPhaseRunning {
				if _, ok := mgr.readyTimes[mytkg_cluster.GetName()]; !ok {
					mgr.readyTimes[mytkg_cluster.GetName()] = metav1.Now()
					log.Info("TKG Cluster Running....")
				}
			} else if mytkg_cluster.Status.Phase == v1alpha1.TanzuKubernetesClusterPhaseCreating {
				if _, ok := mgr.startTimes[mytkg_cluster.GetName()]; !ok {
					mgr.startTimes[mytkg_cluster.GetName()] = metav1.Now()
					log.Info("TKG Cluster Creation Started....")
				}
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
func (mgr *TkgManager) Init(
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
func (mgr *TkgManager) Create(spec interface{}) error {
	tkgspec := spec.(*v1alpha1.TanzuKubernetesCluster)
	switch s := spec.(type) {
	default:
		//log.Errorf("Invalid spec type %T for tkg create action.", s)
		return fmt.Errorf("Invalid spec type %T for tkg create action.", s)
	case *v1alpha1.TanzuKubernetesCluster:
		tid, _ := strconv.Atoi(s.Labels["tid"])
		cid := tid % len(mgr.clientsets)

		ns := mgr.namespace
		if s.Namespace != "" {
			ns = s.Namespace
			mgr.tkgMutex.Lock()
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
			mgr.tkgMutex.Unlock()
		}

		startTime := metav1.Now()
		err := mgr.clientsets[cid].Create(context.TODO(), tkgspec)
		latency := metav1.Now().Time.Sub(startTime.Time).Round(time.Microsecond)

		if err != nil {
			fmt.Printf("Error ",err)
			panic(err)
		}

		mgr.alMutex.Lock()
		mgr.apiTimes[CREATE_ACTION] = append(mgr.apiTimes[CREATE_ACTION], latency)
		mgr.alMutex.Unlock()

		mgr.tkgMutex.Lock()
		fmt.Printf("TKG Cluster name %v \n", tkgspec.GetName())
		mgr.tkgNs[tkgspec.GetName()] = ns
		mgr.tkgMutex.Unlock()
	}
	return nil
}

/*
 * This function implements the LIST action.
 */
func (mgr *TkgManager) List(n interface{}) error {
	log.Info("Nedd to implement this later!!....")
	return nil
}

/*
 * This function implements the DELETE action.
 */
func (mgr *TkgManager) Delete(n interface{}) error {
	log.Info("Nedd to implement this later!!....")
	return nil
}

/*
 * This function implements the DeleteAll manager interface. It is used to clean
 * all the resources that are created by the Tkg manager.
 */
func (mgr *TkgManager) DeleteAll() error {
	log.Info("Nedd to implement this later!!....")
	return nil
}

/*
 * This function returns whether all the created Tkgs become ready
 */
func (mgr *TkgManager) IsStable() bool {
	return len(mgr.readyTimes) == len(mgr.apiTimes[CREATE_ACTION])
}

/*
 * This function computes all the metrics and stores the results into the log file.
 */
func (mgr *TkgManager) LogStats() {
	log.Infof("------------------------------------ TKG Operation Summary " +
		"-----------------------------------")
	log.Infof("%-50v %-10v", "Number of valid tkg requests:",
		len(mgr.apiTimes[CREATE_ACTION]))
	log.Infof("%-50v %-10v", "Number of create tkg:", len(mgr.startTimes))
	log.Infof("%-50v %-10v", "Number of tkgs running:", len(mgr.readyTimes))

	log.Infof("%-50v %-10v", "tkg creation throughput (tkgs/minutes):",
		mgr.tkgThroughput)
	log.Infof("%-50v %-10v", "tkg creation average latency:",
		mgr.tkgAvgLatency)

	log.Infof("--------------------------------- Tkg Startup Latencies (ms) " +
		"---------------------------------")
	log.Infof("%-50v %-10v %-10v %-10v %-10v", " ", "median", "min", "max", "99%")

	var latency perf_util.OperationLatencyMetric
	latency = mgr.startToReadyLatency
	if latency.Valid {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Tkg creation latency stats (client): ",
			latency.Latency.Mid, latency.Latency.Min, latency.Latency.Max, latency.Latency.P99)
	} else {
		log.Infof("%-50v %-10v %-10v %-10v %-10v",
			"Tkg creation latency stats (client): ",
			"---", "---", "---", "---")
	}

	log.Infof("--------------------------------- Tkg API Call Latencies (ms) " +
		"--------------------------------")
	log.Infof("%-50v %-10v %-10v %-10v %-10v", " ", "median", "min", "max", "99%")

	var mid, min, max, p99 float32
	for m, _ := range mgr.apiTimes {
		mid = float32(mgr.apiTimes[m][len(mgr.apiTimes[m])/2]) / float32(time.Millisecond)
		min = float32(mgr.apiTimes[m][0]) / float32(time.Millisecond)
		max = float32(mgr.apiTimes[m][len(mgr.apiTimes[m])-1]) / float32(time.Millisecond)
		p99 = float32(mgr.apiTimes[m][len(mgr.apiTimes[m])-1-len(mgr.apiTimes[m])/100]) /
			float32(time.Millisecond)
		log.Infof("%-50v %-10v %-10v %-10v %-10v", m+" Tkg latency: ", mid, min, max, p99)
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

func (mgr *TkgManager) GetResourceName(userTkgPrefix string, opNum int, tid int) string {
	if userTkgPrefix == "" {
		return tkgNamePrefix + "oid-" + strconv.Itoa(opNum) + "-tid-" + strconv.Itoa(tid)
	} else {
		return userTkgPrefix + "-" + tkgNamePrefix + "oid-" + strconv.Itoa(opNum) + "-tid-" + strconv.Itoa(tid)
	}
}

func (mgr *TkgManager) SendMetricToWavefront(
	now time.Time,
	wfTags []perf_util.WavefrontTag,
	wavefrontPathDir string,
	prefix string) {

	log.Info("Current Not Supported For Tkg Operations!!...")

}

// Get op num given Tkg name
func (mgr *TkgManager) getOpNum(name string) int {
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

func (mgr *TkgManager) CalculateStats() {
	latPerOp := make(map[int][]float32, 0)
	var totalLat float32
	totalLat = 0.0
	tkgCount := 0
	// The below loop groups the latency by operation
	for p, ct := range mgr.readyTimes {
		opn := mgr.getOpNum(p)
		if opn == -1 {
			continue
		}
		nl := float32(ct.Time.Sub(mgr.startTimes[p].Time)) / float32(time.Second)
		latPerOp[opn] = append(latPerOp[opn], nl)
		totalLat += nl
		tkgCount += 1
	}

	var accStartTime float32
	accStartTime = 0.0
	acctkgs := 0

	for opn, _ := range latPerOp {
		sort.Slice(latPerOp[opn],
			func(i, j int) bool { return latPerOp[opn][i] < latPerOp[opn][j] })

		curLen := len(latPerOp[opn])
		accStartTime += float32(latPerOp[opn][curLen/2])
		acctkgs += (curLen + 1) / 2
	}

	mgr.tkgAvgLatency = totalLat / float32(tkgCount)
	mgr.tkgThroughput = float32(acctkgs) * float32(60) / accStartTime

	startToReadyLatency := make([]time.Duration, 0)

	for p, ct := range mgr.startTimes {
		if st, ok := mgr.readyTimes[p]; ok {
			startToReadyLatency = append(startToReadyLatency, st.Time.Sub(ct.Time).
				Round(time.Microsecond))
		}
	}

	sort.Slice(startToReadyLatency,
		func(i, j int) bool { return startToReadyLatency[i] < startToReadyLatency[j] })
	
	var mid, min, max, p99 float32

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

func (mgr *TkgManager) CalculateSuccessRate() int {
	if len(mgr.startTimes) == 0 {
		return 0
	}
	return len(mgr.readyTimes) * 100 / len(mgr.startTimes)
}