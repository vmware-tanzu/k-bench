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
	"context"
	log "github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"os/exec"
	"strings"
	"time"
)

func HandlePredicate(
	clientset *kubernetes.Clientset,
	client dynamic.Interface,
	config *restclient.Config,
	ps PredicateSpec,
	interval int,
	timeout int) bool {
	totalWait := 0
	for totalWait < timeout {
		ok := checkPredicateOk(clientset, client, config, ps)
		if !ok {
			log.Infof("Predicate not passed, sleep %v mili-seconds", interval)
			time.Sleep(time.Duration(interval) * time.Millisecond)
			totalWait += interval
		} else {
			log.Info("Predicate check passed, continue execution...")
			return true
		}
	}
	log.Info("Predicate processing timed out, continue the operation...")
	return false
}

// This function checks the given predicate and return true if it is ok to proceed
func checkPredicateOk(clientset *kubernetes.Clientset,
	client dynamic.Interface,
	config *restclient.Config,
	ps PredicateSpec) bool {

	gvk, ns, resName, cName := getResourceInfo(ps)
	if (gvk == nil || ns == "") && ps.Command == "" {
		log.Warnf("No valid predicate found, proceed...")
		return true
	}

	var objs []*unstructured.Unstructured
	var container *apiv1.Container
	// check resources predicate
	if gvk != nil {
		gvr, _ := meta.UnsafeGuessKindToResource(*gvk)

		if resName != "" {
			// If resource name is specified, labels are ignored.
			obj, _ := client.Resource(gvr).Namespace(ns).Get(context.Background(), resName, metav1.GetOptions{})
			if obj == nil {
				log.Warnf("Resource predicate not passed, object not found...")
				return false
			}

			objs = append(objs, obj)
			if cName != "" {
				// At this point we need to make sure the retrieved resource is a pod
				podContent := obj.UnstructuredContent()
				var p apiv1.Pod
				e := runtime.DefaultUnstructuredConverter.FromUnstructured(podContent, &p)
				if e != nil {
					log.Errorf("Container is specified, but can't get pod, predicate ignored.")
					return true
				}
				// If container is specified, an implicit predicate is that the pod has to be running
				if p.Status.Phase != apiv1.PodRunning {
					log.Warnf("Resource predicate not passed, pod is not running...")
					return false
				}
				for _, c := range p.Spec.Containers {
					if c.Name == cName {
						container = &c
						break
					}
				}
			}
		} else {
			// If resource name is not specified, use labels to retrieve resources.
			options := metav1.ListOptions{}
			if ps.Labels != "" {
				options = getListOptions(ps.Labels)
			}
			objList, _ := client.Resource(gvr).Namespace(ns).List(context.Background(), options)
			if objList == nil || len(objList.Items) == 0 {
				log.Warnf("Resource predicate not passed, no resources found for the given kind and namespace...")
				return false
			}
			for _, item := range objList.Items {
				objs = append(objs, &item)
			}
		}
	}

	if ps.Command == "" {
		return true
	}

	// Check command predicate
	if container != nil && container.Name == cName {
		return runCmdInContainerPredicate(clientset, config, ps.Command, ps.Expect, ns, resName, container)
	} else if cName == "" {
		return runCmdPredicate(ps.Command, ps.Expect)
	} else {
		// Container and command is specified but the container can not be found
		log.Errorf("Container is specified, but can't be found, command predicate ignored.")
		return true
	}
}

func runCmdPredicate(command string, expect string) bool {
	var cmd *exec.Cmd
	cmdarr := strings.Split(command, " ")
	if len(cmdarr) == 1 {
		cmd = exec.Command(command)
	} else {
		cmd = exec.Command(cmdarr[0], cmdarr[1:]...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Errorf("Error running predicate command %v, predicate ignored. Error: ", command, err)
		return true
	}
	outStr := string(out)
	return isPredicateExpected(outStr, expect)
}

func isPredicateExpected(output string, expect string) bool {
	pos := strings.Index(expect, ":")
	if pos == -1 {
		log.Errorf("Wrong predicate expect: %v, predicate ignored.", expect)
		return true
	}
	cond := expect[0:pos]
	str := expect[pos+1:]
	if cond == "contains" {
		if strings.Contains(output, str) {
			log.Info("Command predicate passed, proceed...")
			return true
		} else {
			log.Warnf("Command predicate not passed, output does not contain specified string")
			return false
		}
	} else if cond == "!contains" {
		if !strings.Contains(output, str) {
			log.Info("Command predicate passed, proceed...")
			return true
		} else {
			log.Warnf("Command predicate not passed, output contains specified string")
			return false
		}
	} else {
		log.Errorf("Wrong predicate expect condition: %v, predicate ignored.", expect)
		return true
	}
}

func runCmdInContainerPredicate(
	client *kubernetes.Clientset,
	config *restclient.Config,
	command string, expect string,
	ns string, podName string,
	container *apiv1.Container) bool {
	cmdarr := strings.Split(command, " ")
	runrequest := client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(ns).
		SubResource("exec").
		Param("container", container.Name)
	runrequest.VersionedParams(&apiv1.PodExecOptions{
		Container: container.Name,
		Command:   cmdarr,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, scheme.ParameterCodec)
	var mystdout, mystderr bytes.Buffer
	exec, err := remotecommand.NewSPDYExecutor(config,
		"POST", runrequest.URL())
	if err != nil {
		log.Errorf("Error running predicate command %v in container, predicate ignored.", command, err)
		return true
	}

	exec.Stream(remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &mystdout,
		Stderr: &mystderr,
		Tty:    false,
	})
	log.Infof("Container %v on pod %v, Run out: %v err: %v",
		container.Name, podName, mystdout.String(),
		mystderr.String())

	return isPredicateExpected(mystdout.String(), expect)
}

func getResourceInfo(ps PredicateSpec) (*schema.GroupVersionKind, string, string, string) {
	paths := strings.Split(ps.Resource, "/")
	hasGv := strings.Contains(ps.Resource, "namespaces")
	var group, version, kind, namespace string
	resName := ""
	containerName := ""
	if hasGv && len(paths) >= 5 && len(paths) <= 7 && paths[2] == "namespaces" {
		// Resource is identified by group/version/namespaces/namespace/kind/[object_name/][container_name]
		group = paths[0]
		version = paths[1]
		kind = paths[4]
		namespace = paths[3]
		if len(paths) > 5 {
			resName = paths[5]
		}
		if len(paths) > 6 {
			containerName = paths[6]
		}
	} else if !hasGv && len(paths) >= 2 && len(paths) <= 4 {
		// Resource is identified by namespace/kind/[object_name/][container_name]
		group = ""
		version = "v1"
		kind = paths[1]
		namespace = paths[0]
		if len(paths) > 2 {
			resName = paths[2]
		}
		if len(paths) > 3 {
			containerName = paths[3]
		}
	} else {
		if ps.Resource != "" {
			log.Errorf("Wrong format in predicate, use one of the two:" +
				"namespace/kind/[object_name/][container_name] or " +
				"group/version/namespaces/namespace/kind/[object_name/][container_name]")
		}
		return nil, "", "", ""
	}

	gvk := &schema.GroupVersionKind{Group: group, Version: version, Kind: kind}
	return gvk, namespace, resName, containerName
}

func getListOptions(s string) metav1.ListOptions {
	filters := make(map[string]string, 0)
	predLabels := strings.Split(s, ";")
	for _, label := range predLabels {
		currLabel := strings.Split(label, "=")

		if currLabel[0] == "oid" {
			filters["opnum"] = currLabel[1]
		} else {
			filters[currLabel[0]] = currLabel[1]
		}
	}
	selector := labels.Set(filters).AsSelector().String()
	options := metav1.ListOptions{LabelSelector: selector}
	return options
}
