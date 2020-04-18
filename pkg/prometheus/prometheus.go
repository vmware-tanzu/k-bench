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

package prometheus

import (
	log "github.com/sirupsen/logrus"
	"k-bench/util"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"os/exec"
	"strings"
	"time"
)

/*
 * PrometheusController controls prometheus stack for the cluster
 */
type PrometheusController struct {
	// This is the client used to manage prometheus stack on the cluster
	client *kubernetes.Clientset

	dynClient *dynamic.Interface

	kubeConfig *restclient.Config

	testConfig *util.TestConfig
}

func NewPrometheusController(c *kubernetes.Clientset,
	dc *dynamic.Interface, kc *restclient.Config,
	tc *util.TestConfig) PrometheusController {
	return PrometheusController{
		client:     c,
		dynClient:  dc,
		kubeConfig: kc,
		testConfig: tc,
	}
}

/*
 * This function setup prometheus stack by applying the specified prometheus manifests using kubectl.
 * TODO: Add a way to create prometheus objects using dynamic client in generic resource manager
 */
func (controller *PrometheusController) EnablePrometheus() {
	manifests := controller.testConfig.PrometheusManifestPaths
	log.Info("Setting up Prometheus on the cluster...")

	prometheusPred := util.PredicateSpec{Command: "kubectl get pods --all-namespaces",
		Expect: "!contains:prometheus"}
	noPrometheus := util.HandlePredicate(controller.client, *controller.dynClient,
		controller.kubeConfig, prometheusPred, 1000, 2000)

	if !noPrometheus {
		log.Warnf("Prometheus may be already installed, skipped prometheus enabling.")
		return
	}

	for _, mf := range manifests {
		cmd := exec.Command("kubectl", "create", "-f", mf)
		out, err := cmd.CombinedOutput()
		outStr := strings.ToLower(string(out))
		if err != nil || strings.Contains(outStr, "error") {
			log.Errorf("Error while enabling prometheus, %v", err)
			break
		}
		// TODO: replace below with predicate
		log.Info("Sleep 5 seconds after each step...")
		time.Sleep(time.Duration(5) * time.Second)
	}
}
