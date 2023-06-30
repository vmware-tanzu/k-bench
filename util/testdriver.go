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

package util

import (
	"bytes"
	"io/ioutil"
	"k-bench/manager"
	"reflect"
	"strings"
	"sync"
	"time"

	"k8s.io/client-go/dynamic"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"

	//"k8s.io/client-go/tools/clientcmd"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"k-bench/perf_util"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	podNamespace string = "kbench-pod-namespace"
	depNamespace string = "kbench-deployment-namespace"
	ssNamespace  string = "kbench-statefulset-namespace"
	svcNamespace string = "kbench-service-namespace"
	rcNamespace  string = "kbench-rc-namespace"
	resNamespace string = "kbench-resource-namespace"
)

const (
	podType string = "kbench-pod"
	depType string = "kbench-deployment-pod"
	ssType  string = "kbench-statefulset-pod"
	rcType  string = "kbench-rc-pod"
	svcType string = "kbench-service"
	nsType  string = "kbench-namespace"
	resType string = "kbench-resource"
)

const routinesPerClient int = 4
const defaultInterval int = 3000
const defaultTimeout int = 1000000
const successRateThreshold int = 90

var wg sync.WaitGroup

var benchConfig TestConfig

var outDir *string

var mgrs map[string]manager.Manager

func Run(kubeConfig *restclient.Config,
	testConfig TestConfig, outputDir *string) error {

	outDir = outputDir
	wcpOps := testConfig.Operations
	dockerCompose := testConfig.DockerCompose

	if dockerCompose != "" {
		//var spec *extv1beta1.Deployment
		log.Info("find docker compose file")
		docker_compose_file := dockerCompose

		cmd := exec.Command("kompose", "convert", "-f", docker_compose_file)
		out, err := cmd.CombinedOutput()
		Output := string(out)
		lines := strings.Split(Output, "\n")
		for _, line := range lines {
			start := strings.Index(line, "Kubernetes file ") + len("Kubernetes file ") + 1
			end := strings.Index(line, "created") - 2
			if start >= 0 && end >= 0 && end > start {
				convertedYamlFile := line[start:end]
				log.Info("Found converted file: " + convertedYamlFile)
				//move config file into config folder
				yamlSpec := "./config/" + convertedYamlFile
				err = os.Rename(convertedYamlFile, yamlSpec)
				if err != nil {
					log.Fatal(err)
				}
				var wcpOp WcpOp
				if strings.Index(yamlSpec, "deployment.yaml") >= 0 {

					input, err := ioutil.ReadFile(yamlSpec)
					if err != nil {
						log.Fatalln(err)
					}

					lines := strings.Split(string(input), "\n")
					//Replace the apiVersion if it is not v1
					for i, line := range lines {
						if strings.Contains(line, "extensions/v1beta1") {
							lines[i] = "apiVersion: apps/v1"
						}
					}
					output := strings.Join(lines, "\n")
					err = ioutil.WriteFile(yamlSpec, []byte(output), 0644)
					if err != nil {
						log.Fatalln(err)
					}

					var actions []Action

					var deploymentCreateAction Action
					deploymentCreateAction.Act = "CREATE"
					deploymentCreateAction.Spec.YamlSpec = yamlSpec
					actions = append(actions, deploymentCreateAction)

					wcpOp.Deployment.Actions = actions
					wcpOp.Deployment.Count = 1

					/*
					 * TODO: Currently we only support CREATE action for docker compose
					 * converted Deployment. We can append other actions and specify sleep
					 * times after each action in wcpOp.Deployment.SleepTimes when we
					 * support those configurations for docker compose in the future.
					 */

				} else if strings.Index(yamlSpec, "service.yaml") >= 0 {
					var actions []Action

					var serviceCreateAction Action
					serviceCreateAction.Act = "CREATE"
					serviceCreateAction.Spec.YamlSpec = yamlSpec
					actions = append(actions, serviceCreateAction)

					wcpOp.Service.Actions = actions
					wcpOp.Service.Count = 1

				}
				wcpOps = append(wcpOps, wcpOp)

			}
		}

		if err != nil {
			log.Fatal(err)
		}

	}
	driverClient, de := kubernetes.NewForConfig(kubeConfig)
	dynClient, dyne := dynamic.NewForConfig(kubeConfig)

	if de != nil || dyne != nil {
		panic(de)
	}

	mgrs = make(map[string]manager.Manager, 0)
	benchConfig = testConfig

	maxClients := make(map[string]int, 0)

	// Find the maximum number of clients needed for each resource type
	for _, op := range wcpOps {
		resourceOps := reflect.ValueOf(op)
		typeOps := resourceOps.Type()
		for i := 0; i < resourceOps.NumField(); i++ {
			if _, exist := NonResourceConfigs[typeOps.Field(i).Name]; !exist {
				count := resourceOps.Field(i).FieldByName("Count").Int()
				if count > 0 {
					resName := typeOps.Field(i).Name
					// Use the generic name if it is not supported by a specific resource manager
					if _, ok := manager.Managers[resName]; !ok {
						resName = "Resource"
					}
					numClients := (int(count)-1)/routinesPerClient + 1
					if m, ok := maxClients[resName]; ok {
						if numClients > m {
							maxClients[resName] = numClients
						}
					} else {
						maxClients[resName] = numClients
					}
				}
			}
		}
	}

	// Normalize ops into new WcpOp array if RepeatTimes is not zero
	normalizedOps := make([]WcpOp, 0)
	for _, op := range wcpOps {
		normalizedOps = append(normalizedOps, op)
		for i := 0; i < op.RepeatTimes; i++ {
			normalizedOps = append(normalizedOps, op)
		}
	}

	startIdx := 0
	startTime := metav1.Now()
	var minutesElapsed time.Duration
	for {
		// Loop through each and every ops
		for opIdx, op := range normalizedOps {
			if op.Predicate.Command != "" || op.Predicate.Resource != "" {
				HandlePredicate(driverClient, dynClient, kubeConfig, op.Predicate, defaultInterval, defaultTimeout)
			}

			opIdx += startIdx

			// Check and run if valid pod config is found
			lastPodAction := checkAndRunPod(kubeConfig, op, opIdx, maxClients)

			// Check and run if valid deployment config is found
			lastDepAction := checkAndRunDeployment(kubeConfig, op, opIdx, maxClients)

			// Check and run if valid statefulset config is found
			lastSsAction := checkAndRunStatefulSet(kubeConfig, op, opIdx, maxClients)

			// Check and run if valid namespace config is found
			checkAndRunNamespace(kubeConfig, op, opIdx, maxClients)

			// Check and run if valid service config is found
			checkAndRunService(kubeConfig, op, opIdx, maxClients)

			// Check and run if valid replication controller config is found
			lastRcAction := checkAndRunRc(kubeConfig, op, opIdx, maxClients)

			// Check and run actions for other type of resources
			checkAndRunResource(kubeConfig, op, opIdx, maxClients)

			log.Infof("Waiting all threads to finish on the current operation")
			//time.Sleep(time.Duration(op.SleepTime) * time.Millisecond)

			wg.Wait()

			minutesElapsed = metav1.Now().Time.Sub(startTime.Time).Round(time.Minute)
			if testConfig.RuntimeInMinutes != 0 && int(minutesElapsed.Minutes()) > testConfig.RuntimeInMinutes {
				log.Info("K-Bench running time reached limit, stop running further operations.")
				break
			}

			// If the benchmark is configured to run in blocking mode, wait when necessary
			if testConfig.BlockingLevel == "operation" {
				interval := defaultInterval
				totalWait := 0
				timeout := defaultTimeout

				if testConfig.CheckingInterval >= 1000 {
					interval = testConfig.CheckingInterval
				}

				if testConfig.Timeout >= 1000 {
					timeout = testConfig.Timeout
				}

				totalWait += waitForPodRelatedOps(mgrs, driverClient, manager.POD,
					lastPodAction, timeout, interval, opIdx)
				if totalWait < timeout {
					totalWait += waitForPodRelatedOps(mgrs, driverClient, manager.DEPLOYMENT,
						lastDepAction, timeout, interval, opIdx)
				}
				if totalWait < timeout {
					totalWait += waitForPodRelatedOps(mgrs, driverClient, manager.REPLICATION_CONTROLLER,
						lastRcAction, timeout, interval, opIdx)
				}
				if totalWait < timeout {
					totalWait += waitForPodRelatedOps(mgrs, driverClient, manager.STATEFUL_SET,
						lastSsAction, timeout, interval, opIdx)
				}

				if totalWait >= timeout {
					log.Warningf("Timed out after waiting %v mili-seconds, moving forward...", timeout)
				}
			}

			if opIdx == len(normalizedOps)-1 {
				log.Infof("All operations completed.")
			} else {
				log.Infof("One operation completed. Continue to run the next...")
			}
		}
		if testConfig.RuntimeInMinutes == 0 || int(minutesElapsed.Minutes()) > testConfig.RuntimeInMinutes {
			break
		}
		startIdx += len(normalizedOps)
	}

	Finalize()

	return nil
}

// This cleans up previously created objects/namespaces, and print results.
func Finalize() {
	// TODO: stop waverunner if user started it

	var wfTags []perf_util.WavefrontTag

	if len(benchConfig.Tags) > 0 {
		wfTags = benchConfig.Tags
	}

	if benchConfig.Cleanup {
		log.Infof("Start to delete all resources for cleanup...")

		for _, mgr := range mgrs {
			mgr.DeleteAll()
		}
	}

	now := time.Now()

	for resource, mgr := range mgrs {
		mgr.CalculateStats()
		printBoundary()
		var b bytes.Buffer

		for i := 0; i < (84-len(resource))/2; i++ {
			b.WriteString("*")
		}
		log.Infof("%-80v", b.String()+" "+resource+
			" Results "+b.String())
		printBoundary()
		mgr.LogStats()

		var successRate int
		if _, ok := manager.PodRelatedResources[resource]; ok {
			successRate = mgr.CalculateSuccessRate()
			log.Infof("Pod Creation Success Rate: %v %%", successRate)
		}

		wavefrontPathDir := benchConfig.WavefrontPathDir

		// Generate wavefront output if Wavefront is configured
		if len(wavefrontPathDir) != 0 {
			_, err := os.Stat(wavefrontPathDir)
			if err != nil {
				// If the given path is not valid, use the current working directory
				dir, error := filepath.Abs(filepath.Dir(os.Args[0]))
				if error != nil {
					log.Fatal(error)
					return
				}
				wavefrontPathDir = dir
			}

			if _, ok := manager.PodRelatedResources[resource]; ok {
				if successRate > successRateThreshold {
					mgr.SendMetricToWavefront(now, wfTags, wavefrontPathDir, "")
				}
			} else {
				mgr.SendMetricToWavefront(now, wfTags, wavefrontPathDir, "")
			}
		}
	}
}
