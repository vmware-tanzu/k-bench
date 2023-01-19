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
	"k8s.io/apimachinery/pkg/runtime/serializer"
	log "github.com/sirupsen/logrus"
	"context"
	"io/ioutil"
	"k-bench/manager"
	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"reflect"
	"strconv"
	"strings"
	"time"
	"github.com/vmware-tanzu/vm-operator-api/api/v1alpha1"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"
)

func checkAndRunVM(
	kubeConfig *restclient.Config,
	op WcpOp,
	opIdx int,
	maxClients map[string]int) string {
	if len(op.VirtualMachine.Actions) > 0 {
		var vmMgr *manager.VmManager

		if mgr, ok := mgrs[manager.VIRTUALMACHINE]; ok {
			vmMgr = mgr.(*manager.VmManager)
		} else {
			mgrs[manager.VIRTUALMACHINE], _ = manager.GetManager(manager.VIRTUALMACHINE)
			vmMgr = mgrs[manager.VIRTUALMACHINE].(*manager.VmManager)
			vmMgr.Init(kubeConfig, vmNamespace, true,
				maxClients[manager.VIRTUALMACHINE], vmType)
		}

		log.Infof("Performing VM actions in operation %v", opIdx)

		for i := 0; i < op.VirtualMachine.Count; i++ {
			go runVmActions(vmMgr, op.VirtualMachine, opIdx, i)
			wg.Add(1)
		}

		return op.VirtualMachine.Actions[len(op.VirtualMachine.Actions)-1].Act
	}
	return ""
}


func checkAndRunPod(
	kubeConfig *restclient.Config,
	op WcpOp,
	opIdx int,
	maxClients map[string]int) string {
	if len(op.Pod.Actions) > 0 {

		var podMgr *manager.PodManager

		if mgr, ok := mgrs[manager.POD]; ok {
			podMgr = mgr.(*manager.PodManager)
		} else {
			mgrs[manager.POD], _ = manager.GetManager(manager.POD)
			podMgr = mgrs[manager.POD].(*manager.PodManager)
			podMgr.Init(kubeConfig, podNamespace, true,
				maxClients[manager.POD], podType)
		}

		log.Infof("Performing pod actions in operation %v", opIdx)

		for i := 0; i < op.Pod.Count; i++ {
			go runPodActions(podMgr, op.Pod, opIdx, i)
			wg.Add(1)
		}

		return op.Pod.Actions[len(op.Pod.Actions)-1].Act
	}
	return ""
}

func checkAndRunDeployment(
	kubeConfig *restclient.Config,
	op WcpOp,
	opIdx int,
	maxClients map[string]int) string {
	if len(op.Deployment.Actions) > 0 {

		var depMgr *manager.DeploymentManager

		if mgr, ok := mgrs[manager.DEPLOYMENT]; ok {
			depMgr = mgr.(*manager.DeploymentManager)
		} else {
			mgrs[manager.DEPLOYMENT], _ = manager.GetManager(manager.DEPLOYMENT)
			depMgr = mgrs[manager.DEPLOYMENT].(*manager.DeploymentManager)
			depMgr.Init(kubeConfig, depNamespace,
				maxClients[manager.DEPLOYMENT], depType)
		}

		log.Infof("Performing deployment actions in operation %v", opIdx)

		for i := 0; i < op.Deployment.Count; i++ {
			go runDeploymentActions(depMgr, op.Deployment, opIdx, i)
			wg.Add(1)
		}

		return op.Deployment.Actions[len(op.Deployment.Actions)-1].Act
	}
	return ""
}

func checkAndRunStatefulSet(
	kubeConfig *restclient.Config,
	op WcpOp,
	opIdx int,
	maxClients map[string]int) string {
	if len(op.StatefulSet.Actions) > 0 {

		var ssMgr *manager.StatefulSetManager

		if mgr, ok := mgrs[manager.STATEFUL_SET]; ok {
			ssMgr = mgr.(*manager.StatefulSetManager)
		} else {
			mgrs[manager.STATEFUL_SET], _ = manager.GetManager(manager.STATEFUL_SET)
			ssMgr = mgrs[manager.STATEFUL_SET].(*manager.StatefulSetManager)
			ssMgr.Init(kubeConfig, ssNamespace,
				maxClients[manager.STATEFUL_SET], ssType)
		}

		log.Infof("Performing statefulset actions in operation %v", opIdx)

		for i := 0; i < op.StatefulSet.Count; i++ {
			go runStatefulSetActions(ssMgr, op.StatefulSet, opIdx, i)
			wg.Add(1)
		}

		return op.StatefulSet.Actions[len(op.StatefulSet.Actions)-1].Act
	}
	return ""
}

func checkAndRunNamespace(
	kubeConfig *restclient.Config,
	op WcpOp,
	opIdx int,
	maxClients map[string]int) {
	if op.Namespace.Count != 0 && len(op.Namespace.Actions) > 0 {

		var nsMgr *manager.NamespaceManager

		if mgr, ok := mgrs[manager.NAMESPACE]; ok {
			nsMgr = mgr.(*manager.NamespaceManager)
		} else {
			mgrs[manager.NAMESPACE], _ = manager.GetManager(manager.NAMESPACE)
			nsMgr = mgrs[manager.NAMESPACE].(*manager.NamespaceManager)
			nsMgr.Init(kubeConfig, "", maxClients[manager.NAMESPACE])
		}

		log.Infof("Performing namespace actions in operation %v", opIdx)

		//wg.Add(op.Namespace.Count)

		for i := 0; i < op.Namespace.Count; i++ {
			go runNamespaceActions(nsMgr, op.Namespace, opIdx, i)
			wg.Add(1)
		}
	}
}

func checkAndRunService(
	kubeConfig *restclient.Config,
	op WcpOp,
	opIdx int,
	maxClients map[string]int) {
	if op.Service.Count != 0 && len(op.Service.Actions) > 0 {

		var svcMgr *manager.ServiceManager

		if mgr, ok := mgrs[manager.SERVICE]; ok {
			svcMgr = mgr.(*manager.ServiceManager)
		} else {
			mgrs[manager.SERVICE], _ = manager.GetManager(manager.SERVICE)
			svcMgr = mgrs[manager.SERVICE].(*manager.ServiceManager)
			svcMgr.Init(kubeConfig, svcNamespace, true, maxClients[manager.SERVICE])
		}

		log.Infof("Performing service actions in operation %v", opIdx)

		for i := 0; i < op.Service.Count; i++ {
			go runServiceActions(svcMgr, op.Service, opIdx, i)
			wg.Add(1)
		}
	}
}

func checkAndRunRc(
	kubeConfig *restclient.Config,
	op WcpOp,
	opIdx int,
	maxClients map[string]int) string {
	if len(op.ReplicationController.Actions) > 0 {

		var rcMgr *manager.ReplicationControllerManager

		if mgr, ok := mgrs[manager.REPLICATION_CONTROLLER]; ok {
			rcMgr = mgr.(*manager.ReplicationControllerManager)
		} else {
			mgrs[manager.REPLICATION_CONTROLLER], _ = manager.GetManager(manager.REPLICATION_CONTROLLER)
			rcMgr = mgrs[manager.REPLICATION_CONTROLLER].(*manager.ReplicationControllerManager)
			rcMgr.Init(kubeConfig, rcNamespace, maxClients[manager.REPLICATION_CONTROLLER],
				rcType)
		}

		log.Infof("Performing replication controller actions in operation %v", opIdx)

		for i := 0; i < op.ReplicationController.Count; i++ {
			go runRcActions(rcMgr, op.ReplicationController, opIdx, i)
			wg.Add(1)
		}

		return op.ReplicationController.
			Actions[len(op.ReplicationController.Actions)-1].Act
	}
	return ""
}

// This is a function to process generic types of resources with a limited set of metrics
func checkAndRunResource(
	kubeConfig *restclient.Config,
	op WcpOp,
	opIdx int,
	maxClients map[string]int) {

	resourceOps := reflect.ValueOf(op)
	typeOps := resourceOps.Type()

	for i := 0; i < resourceOps.NumField(); i++ {
		if _, exist := NonResourceConfigs[typeOps.Field(i).Name]; !exist {
			resource := resourceOps.Field(i).Interface()
			count := int(resourceOps.Field(i).FieldByName("Count").Int())
			if count > 0 {
				resName := typeOps.Field(i).Name
				// Skip the resource kinds with specific managers
				if _, ok := manager.Managers[resName]; !ok {
					var resMgr *manager.ResourceManager

					if mgr, ok := mgrs["Resource"]; ok {
						resMgr = mgr.(*manager.ResourceManager)
					} else {
						mgrs["Resource"], _ = manager.GetManager("Resource")
						resMgr = mgrs["Resource"].(*manager.ResourceManager)
						resMgr.Init(kubeConfig, resNamespace, true, maxClients["Resource"])
					}

					log.Infof("Performing general resource actions in operation %v", opIdx)

					for i := 0; i < count; i++ {
						go runResourceActions(resMgr, resource.(ResourceConfig), opIdx, i, resName)
						wg.Add(1)
					}
				}
			}
		}
	}
}
// Decode a yaml file for custom resources
func decodeYaml_Crd(yamlFile string, kind string) (runtime.Object, error) {
	if yamlFile == "" {
		return nil, nil
	}

	data, de := ioutil.ReadFile(yamlFile)

	if de != nil {
		log.Error(de)
		return nil, de
	} else {
		scheme := runtime.NewScheme()
		if kind == "VirtualMachine" {
			_ = v1alpha1.AddToScheme(scheme)
			decoder := serializer.NewCodecFactory(scheme).UniversalDecoder()
			object := &v1alpha1.VirtualMachine{}
			err := runtime.DecodeInto(decoder, data, object)
			if err != nil {
				return nil, err
			}
			return object, nil
		}
	}
	return nil, nil
}

// Decode a yaml file for resource creation
func decodeYaml(yamlFile string) (runtime.Object, error) {
	if yamlFile == "" {
		return nil, nil
	}

	data, de := ioutil.ReadFile(yamlFile)

	if de != nil {
		log.Error(de)
		return nil, de
	} else {
		decode := scheme.Codecs.UniversalDeserializer().Decode
		obj, _, oe := decode(data, nil, nil)

		if oe != nil {
			log.Error(oe)
			return nil, oe
		} else {
			return obj, nil
		}
	}
}

// Update labels
func updateLabels(
	labels map[string]string,
	resType string,
	opNum int,
	tid int) {
	labels["app"] = manager.AppName
	labels["type"] = resType
	labels["opnum"] = strconv.Itoa(opNum)
	labels["tid"] = strconv.Itoa(tid)
}

// A helper function to update labels and namespace
func updateLabelNs(
	spec ActionSpec,
	kfromconfig string,
	vfromconfig string,
	ns *string,
	lk *string,
	lv *string) {

	if spec.Namespace != "" {
		*ns = spec.Namespace
	}
	if spec.LabelKey != "" && spec.LabelValue != "" {
		*lk = spec.LabelKey
		*lv = spec.LabelValue
	} else {
		*lk = kfromconfig
		*lv = vfromconfig
	}
}

// A function that runs a set of Pod actions
func runPodActions(
	mgr *manager.PodManager,
	podConfig PodConfig,
	opNum int,
	tid int) {

	// Get default pod name (used when filtering not specified or applicable)
	podName := mgr.GetResourceName(podConfig.PodNamePrefix, opNum, tid)

	actions := podConfig.Actions

	created := false

	sleepTimes := podConfig.SleepTimes

	si := 0

	for _, action := range actions {
		var lk, lv string
		ns := podNamespace

		if podConfig.Namespace != "" {
			ns = podConfig.Namespace
		}
		actStr := strings.ToLower(action.Act)

		if actStr == manager.CREATE_ACTION {
			if created {
				log.Warningf("Only one CREATE may be executed in an operation, skipped.")
				continue
			} else {
				created = true
			}

			var spec *apiv1.Pod
			createSpec := action.Spec
			updateLabelNs(createSpec, podConfig.LabelKey, podConfig.LabelValue, &ns, &lk, &lv)

			obj, derr := decodeYaml(createSpec.YamlSpec)
			if obj != nil && derr == nil {
				spec = obj.(*apiv1.Pod)
				if spec.Kind != "Pod" {
					log.Warningf("Invalid kind specified in yaml for pod creation: %v",
						spec.Kind)
					spec = nil
				}
			}

			if spec == nil {
				as := manager.ActionSpec{podName, tid, opNum, ns,
					lk, lv, true, "", manager.POD}

				if createSpec.ImagePullPolicy == "Always" {
					spec = genPodSpec(createSpec.Image, podConfig.ContainerNamePrefix,
						apiv1.PullAlways, opNum, as)
				} else if podConfig.ImagePullPolicy == "Never" {
					spec = genPodSpec(createSpec.Image, podConfig.ContainerNamePrefix,
						apiv1.PullNever, opNum, as)
				} else {
					spec = genPodSpec(createSpec.Image, podConfig.ContainerNamePrefix,
						apiv1.PullIfNotPresent, opNum, as)
				}
			} else {
				// Name from yaml file are not respected to ensure integrity.
				spec.Name = podName

				if spec.Namespace == "" {
					spec.Namespace = ns
				} else {
					ns = spec.Namespace
				}

				for i := 0; i < len(spec.Spec.Containers); i++ {
					spec.Spec.Containers[i].Name = podConfig.ContainerNamePrefix +
						spec.Spec.Containers[i].Name
				}

				if spec.Labels == nil {
					spec.Labels = make(map[string]string, 0)
				}

				updateLabels(spec.Labels, podType, opNum, tid)

				if lk != "" && lv != "" {
					spec.Labels[lk] = lv
				}
			}
			ae := mgr.ActionFuncs[manager.CREATE_ACTION](mgr, spec)
			if ae != nil {
				log.Error(ae)
			}
		} else if actStr == manager.RUN_ACTION {
			runSpec := action.Spec
			updateLabelNs(runSpec, podConfig.LabelKey, podConfig.LabelValue, &ns, &lk, &lv)

			as := manager.ActionSpec{
				podName, tid, opNum, ns, lk, lv,
				runSpec.MatchGoroutine, runSpec.MatchOperation, manager.POD}
			ae := mgr.ActionFuncs[manager.RUN_ACTION](mgr,
				manager.RunSpec{runSpec.Command, as})
			if ae != nil {
				log.Error(ae)
			}
		} else if actStr == manager.COPY_ACTION {
			copySpec := action.Spec
			updateLabelNs(copySpec, podConfig.LabelKey, podConfig.LabelValue, &ns, &lk, &lv)

			as := manager.ActionSpec{
				podName, tid, opNum, ns, lk, lv,
				copySpec.MatchGoroutine, copySpec.MatchOperation, manager.POD}
			ae := mgr.ActionFuncs[manager.COPY_ACTION](mgr,
				manager.CopySpec{
					*outDir,
					copySpec.LocalPath,
					copySpec.ContainerPath,
					copySpec.Upload, as})
			if ae != nil {
				log.Error(ae)
			}
		} else if actionFunc, ok := mgr.ActionFuncs[actStr]; ok {
			lusdSpec := action.Spec
			updateLabelNs(lusdSpec, podConfig.LabelKey, podConfig.LabelValue, &ns, &lk, &lv)

			as := manager.ActionSpec{
				podName, tid, opNum, ns, lk, lv,
				lusdSpec.MatchGoroutine, lusdSpec.MatchOperation, manager.POD}
			ae := actionFunc(mgr, as)

			if ae != nil {
				log.Error(ae)
			}
		}

		// TODO: add optional status checking logic here
		if si < len(sleepTimes) {
			log.Infof("Sleep %v mili-seconds after %v action", sleepTimes[si], action.Act)
			time.Sleep(time.Duration(sleepTimes[si]) * time.Millisecond)
			si++
		}
	}

	wg.Done()
}

func runVmActions(
	mgr *manager.VmManager,
	vmConfig VmConfig,
	opNum int,
	tid int) {

	// Get default vm name (used when filtering not specified or applicable)
	vmName := mgr.GetResourceName(vmConfig.VmNamePrefix, opNum, tid)

	actions := vmConfig.Actions

	created := false

	sleepTimes := vmConfig.SleepTimes

	si := 0

	for _, action := range actions {
		var lk, lv string
		ns := vmNamespace

		if vmConfig.Namespace != "" {
			ns = vmConfig.Namespace
		}
		actStr := strings.ToLower(action.Act)

		if actStr == manager.CREATE_ACTION {
			if created {
				log.Warningf("Only one CREATE may be executed in an operation, skipped.")
				continue
			} else {
				created = true
			}

			var spec *v1alpha1.VirtualMachine
			createSpec := action.Spec
			updateLabelNs(createSpec, vmConfig.LabelKey, vmConfig.LabelValue, &ns, &lk, &lv)

			obj, derr := decodeYaml_Crd(createSpec.YamlSpec, "VirtualMachine")
			if obj != nil && derr == nil {
				spec = obj.(*v1alpha1.VirtualMachine)
				if spec.Kind != "VirtualMachine" {
					log.Warningf("Invalid kind specified in yaml for pod creation: %v",
						spec.Kind)
					spec = nil
				}
			}

			if spec == nil {
				as := manager.ActionSpec{vmName, tid, opNum, ns,
					lk, lv, true, "", manager.VIRTUALMACHINE}			
				spec = genVmSpec(createSpec.ClassName, createSpec.ImageName, createSpec.StorageClass, createSpec.PowerState,
						opNum, as)
			} else {
				//Name from yaml file are not respected to ensure integrity.
				spec.Name = vmName

				if spec.Namespace == "" {
					spec.Namespace = ns
				} else {
					ns = spec.Namespace
				}

				spec.Spec.ClassName = spec.Spec.ClassName
				spec.Spec.ImageName = spec.Spec.ImageName
				spec.Spec.StorageClass = spec.Spec.StorageClass
				spec.Spec.PowerState = spec.Spec.PowerState

				if spec.Labels == nil {
					spec.Labels = make(map[string]string, 0)
				}

				updateLabels(spec.Labels, vmType, opNum, tid)

				if lk != "" && lv != "" {
					spec.Labels[lk] = lv
				}
			}
			ae := mgr.ActionFuncs[manager.CREATE_ACTION](mgr, spec)
			if ae != nil {
				log.Error(ae)
			}
		} else if actionFunc, ok := mgr.ActionFuncs[actStr]; ok {
			lusdSpec := action.Spec
			updateLabelNs(lusdSpec, vmConfig.LabelKey, vmConfig.LabelValue, &ns, &lk, &lv)

			as := manager.ActionSpec{
					vmName, tid, opNum, ns, lk, lv,
					lusdSpec.MatchGoroutine, lusdSpec.MatchOperation, manager.VIRTUALMACHINE}
			ae := actionFunc(mgr, as)

			if ae != nil {
				log.Error(ae)
			}
		}

		// TODO: add optional status checking logic here
		if si < len(sleepTimes) {
			log.Infof("Sleep %v mili-seconds after %v action", sleepTimes[si], action.Act)
			time.Sleep(time.Duration(sleepTimes[si]) * time.Millisecond)
			si++
		}
	}

	//wg.Done()
}

// A function that runs a set of Deployment actions
func runDeploymentActions(
	mgr *manager.DeploymentManager,
	depConfig DeploymentConfig,
	opNum int,
	tid int) {

	// Get default deployment name (used when labels/selectors not specified or applicable)
	depName := mgr.GetResourceName(opNum, tid)

	actions := depConfig.Actions

	created := false

	sleepTimes := depConfig.SleepTimes

	si := 0

	for _, action := range actions {
		var lk, lv string
		ns := depNamespace

		if depConfig.Namespace != "" {
			ns = depConfig.Namespace
		}
		actStr := strings.ToLower(action.Act)

		if actStr == manager.CREATE_ACTION {
			if created {
				log.Warningf("Only one CREATE may be executed in an operation, skipped.")
				continue
			} else {
				created = true
			}

			var spec *appsv1.Deployment
			createSpec := action.Spec
			updateLabelNs(createSpec, depConfig.LabelKey, depConfig.LabelValue, &ns, &lk, &lv)

			obj, derr := decodeYaml(createSpec.YamlSpec)
			if obj != nil && derr == nil {
				spec = obj.(*appsv1.Deployment)
				if spec.Kind != "Deployment" {
					log.Warningf("Invalid kind specified in yaml for deployment creation: %v",
						spec.Kind)
					spec = nil
				}
			}

			if spec == nil {
				as := manager.ActionSpec{depName, tid, opNum, ns, lk,
					lv, true, "", manager.DEPLOYMENT}

				if createSpec.ImagePullPolicy == "Always" {
					spec = genDeploymentSpec(createSpec.Image,
						createSpec.NumReplicas, apiv1.PullAlways,
						opNum, as)
				} else if createSpec.ImagePullPolicy == "Never" {
					spec = genDeploymentSpec(createSpec.Image,
						createSpec.NumReplicas, apiv1.PullNever,
						opNum, as)
				} else {
					spec = genDeploymentSpec(createSpec.Image,
						createSpec.NumReplicas, apiv1.PullIfNotPresent,
						opNum, as)
				}
			} else {
				// Name from yaml file are not respected to ensure integrity.
				spec.Name = depName

				if spec.Namespace == "" {
					spec.Namespace = ns
				} else {
					ns = spec.Namespace
				}

				// Selector is mandatory for apps v1 deployment spec
				if spec.Spec.Selector == nil {
					spec.Spec.Selector = &metav1.LabelSelector{
						MatchLabels: make(map[string]string, 0)}
				}

				updateLabels(spec.Spec.Selector.MatchLabels, depType, opNum, tid)

				if spec.Spec.Template.ObjectMeta.Labels == nil {
					spec.Spec.Template.ObjectMeta.Labels = make(map[string]string, 0)
				}

				updateLabels(spec.Spec.Template.ObjectMeta.Labels, depType, opNum, tid)

				if spec.Labels == nil {
					spec.Labels = make(map[string]string, 0)
				}

				updateLabels(spec.Labels, depType, opNum, tid)

				if lk != "" && lv != "" {
					spec.Spec.Selector.MatchLabels[lk] = lv
					spec.Spec.Template.ObjectMeta.Labels[lk] = lv
					spec.Labels[lk] = lv
				}
			}

			ae := mgr.ActionFuncs[manager.CREATE_ACTION](mgr, spec)

			if ae != nil {
				log.Error(ae)
			}
			// TODO: add RUN action?
		} else if actionFunc, ok := mgr.ActionFuncs[actStr]; ok {
			lusdSpec := action.Spec
			updateLabelNs(lusdSpec, depConfig.LabelKey, depConfig.LabelValue, &ns, &lk, &lv)

			as := manager.ActionSpec{
				depName, tid, opNum, ns, lk, lv,
				lusdSpec.MatchGoroutine, lusdSpec.MatchOperation, manager.DEPLOYMENT}
			ae := actionFunc(mgr, as)

			if ae != nil {
				log.Error(ae)
			}
		}

		if si < len(sleepTimes) {
			log.Infof("Sleep %v mili-seconds after %v action", sleepTimes[si], action.Act)
			time.Sleep(time.Duration(sleepTimes[si]) * time.Millisecond)
			si++
		}
	}

	wg.Done()
}

// A function that runs a set of StatefulSet actions
func runStatefulSetActions(
	mgr *manager.StatefulSetManager,
	ssConfig StatefulSetConfig,
	opNum int,
	tid int) {

	// Get default statefulset name (used when labels/selectors not specified or applicable)
	ssName := mgr.GetResourceName(opNum, tid)

	actions := ssConfig.Actions

	created := false

	sleepTimes := ssConfig.SleepTimes

	si := 0

	for _, action := range actions {
		var lk, lv string
		ns := ssNamespace

		if ssConfig.Namespace != "" {
			ns = ssConfig.Namespace
		}
		actStr := strings.ToLower(action.Act)

		if actStr == manager.CREATE_ACTION {
			if created {
				log.Warningf("Only one CREATE may be executed in an operation, skipped.")
				continue
			} else {
				created = true
			}

			var spec *appsv1.StatefulSet
			createSpec := action.Spec
			updateLabelNs(createSpec, ssConfig.LabelKey, ssConfig.LabelValue, &ns, &lk, &lv)

			obj, derr := decodeYaml(createSpec.YamlSpec)
			if obj != nil && derr == nil {
				spec = obj.(*appsv1.StatefulSet)
				if spec.Kind != "StatefulSet" {
					log.Warningf("Invalid kind specified in yaml for statefulset creation: %v",
						spec.Kind)
					spec = nil
				}
			}

			if spec == nil {
				as := manager.ActionSpec{ssName, tid, opNum, ns, lk,
					lv, true, "", manager.STATEFUL_SET}

				if createSpec.ImagePullPolicy == "Always" {
					spec = genStatefulSetSpec(createSpec.Image,
						createSpec.NumReplicas, apiv1.PullAlways,
						opNum, as)
				} else if createSpec.ImagePullPolicy == "Never" {
					spec = genStatefulSetSpec(createSpec.Image,
						createSpec.NumReplicas, apiv1.PullNever,
						opNum, as)
				} else {
					spec = genStatefulSetSpec(createSpec.Image,
						createSpec.NumReplicas, apiv1.PullIfNotPresent,
						opNum, as)
				}
			} else {
				// Name from yaml file are not respected to ensure integrity.
				spec.Name = ssName

				if spec.Namespace == "" {
					spec.Namespace = ns
				} else {
					ns = spec.Namespace
				}

				// Selector is mandatory for apps v1 deployment spec
				if spec.Spec.Selector == nil {
					spec.Spec.Selector = &metav1.LabelSelector{
						MatchLabels: make(map[string]string, 0)}
				}

				updateLabels(spec.Spec.Selector.MatchLabels, ssType, opNum, tid)

				if spec.Spec.Template.ObjectMeta.Labels == nil {
					spec.Spec.Template.ObjectMeta.Labels = make(map[string]string, 0)
				}

				updateLabels(spec.Spec.Template.ObjectMeta.Labels, ssType, opNum, tid)

				if spec.Labels == nil {
					spec.Labels = make(map[string]string, 0)
				}

				updateLabels(spec.Labels, ssType, opNum, tid)

				if lk != "" && lv != "" {
					spec.Spec.Selector.MatchLabels[lk] = lv
					spec.Spec.Template.ObjectMeta.Labels[lk] = lv
					spec.Labels[lk] = lv
				}
			}

			ae := mgr.ActionFuncs[manager.CREATE_ACTION](mgr, spec)

			if ae != nil {
				log.Error(ae)
			}
			// TODO: add RUN action?
		} else if actionFunc, ok := mgr.ActionFuncs[actStr]; ok {
			lusdSpec := action.Spec
			updateLabelNs(lusdSpec, ssConfig.LabelKey, ssConfig.LabelValue, &ns, &lk, &lv)

			as := manager.ActionSpec{
				ssName, tid, opNum, ns, lk, lv,
				lusdSpec.MatchGoroutine, lusdSpec.MatchOperation, manager.STATEFUL_SET}
			ae := actionFunc(mgr, as)

			if ae != nil {
				log.Error(ae)
			}
		}

		if si < len(sleepTimes) {
			log.Infof("Sleep %v mili-seconds after %v action", sleepTimes[si], action.Act)
			time.Sleep(time.Duration(sleepTimes[si]) * time.Millisecond)
			si++
		}
	}

	wg.Done()
}

// A function that runs a set of Namespace actions
func runNamespaceActions(
	mgr *manager.NamespaceManager,
	nsConfig NamespaceConfig,
	opNum int,
	tid int) {

	nsName := mgr.GetResourceName(opNum, tid)

	actions := nsConfig.Actions

	created := false

	sleepTimes := nsConfig.SleepTimes

	si := 0

	for _, action := range actions {
		var lk, lv string
		actStr := strings.ToLower(action.Act)

		if actStr == manager.CREATE_ACTION {

			if created {
				log.Warningf("Only one CREATE may be executed in an operation, skipped.")
				continue
			} else {
				created = true
			}

			createSpec := action.Spec
			if createSpec.LabelKey != "" && createSpec.LabelValue != "" {
				lk = createSpec.LabelKey
				lv = createSpec.LabelValue
			} else {
				lk = nsConfig.LabelKey
				lv = nsConfig.LabelValue
			}

			spec := genNamespaceSpec(nsName, opNum, tid)

			if spec.Labels == nil {
				spec.Labels = make(map[string]string, 0)
			}

			updateLabels(spec.Labels, nsType, opNum, tid)

			if lk != "" && lv != "" {
				spec.Labels[lk] = lv
			}

			ae := mgr.ActionFuncs[manager.CREATE_ACTION](mgr, spec)
			if ae != nil {
				log.Error(ae)
			}
		} else if actionFunc, ok := mgr.ActionFuncs[actStr]; ok {
			lusdSpec := action.Spec
			as := manager.ActionSpec{
				nsName, tid, opNum, nsName, lk, lv,
				lusdSpec.MatchGoroutine, lusdSpec.MatchOperation, manager.NAMESPACE}
			ae := actionFunc(mgr, as)

			if ae != nil {
				log.Error(ae)
			}
		}

		// TODO: add optional status checking logic here
		if si < len(sleepTimes) {
			log.Infof("Sleep %v mili-seconds after %v action", sleepTimes[si], action.Act)
			time.Sleep(time.Duration(sleepTimes[si]) * time.Millisecond)
			si++
		}
	}

	wg.Done()
}

// A function that runs a set of Service actions
func runServiceActions(
	mgr *manager.ServiceManager,
	svcConfig ServiceConfig,
	opNum int,
	tid int) {

	svcName := mgr.GetResourceName(opNum, tid)

	actions := svcConfig.Actions

	created := false

	sleepTimes := svcConfig.SleepTimes

	si := 0

	for _, action := range actions {
		var lk, lv string
		ns := svcNamespace

		if svcConfig.Namespace != "" {
			ns = svcConfig.Namespace
		}
		actStr := strings.ToLower(action.Act)

		if actStr == manager.CREATE_ACTION {
			if created {
				log.Warningf("Only one CREATE may be executed in an operation, skipped.")
				continue
			} else {
				created = true
			}

			var spec *apiv1.Service
			createSpec := action.Spec
			updateLabelNs(createSpec, svcConfig.LabelKey, svcConfig.LabelValue, &ns, &lk, &lv)

			obj, derr := decodeYaml(createSpec.YamlSpec)
			if obj != nil && derr == nil {
				spec = obj.(*apiv1.Service)
				if spec.Kind != "Service" {
					log.Warningf("Invalid kind specified in yaml for service creation: %v",
						spec.Kind)
					spec = nil
				}
			}

			if spec == nil {
				as := manager.ActionSpec{svcName, tid, opNum, ns, lk,
					lv, true, "", manager.SERVICE}
				spec = genServiceSpec(opNum, as)
			} else {
				// Name from yaml file are not respected to ensure integrity.
				spec.Name = svcName

				if spec.Namespace == "" {
					spec.Namespace = ns
				} else {
					ns = spec.Namespace
				}

				if spec.Spec.Selector == nil {
					spec.Spec.Selector = make(map[string]string, 0)
				}

				spec.Spec.Selector["app"] = manager.AppName

				if spec.Labels == nil {
					spec.Labels = make(map[string]string, 0)
				}

				updateLabels(spec.Labels, svcType, opNum, tid)

				if lk != "" && lv != "" {
					spec.Labels[lk] = lv
				}
			}

			ae := mgr.ActionFuncs[manager.CREATE_ACTION](mgr, spec)

			if ae != nil {
				log.Error(ae)
			}
		} else if actionFunc, ok := mgr.ActionFuncs[actStr]; ok {
			lusdSpec := action.Spec
			updateLabelNs(lusdSpec, svcConfig.LabelKey, svcConfig.LabelValue, &ns, &lk, &lv)

			as := manager.ActionSpec{
				svcName, tid, opNum, ns, lk, lv,
				lusdSpec.MatchGoroutine, lusdSpec.MatchOperation, manager.SERVICE}
			ae := actionFunc(mgr, as)

			if ae != nil {
				log.Error(ae)
			}
		}

		// TODO: add optional status checking logic here
		if si < len(sleepTimes) {
			log.Infof("Sleep %v mili-seconds after %v action", sleepTimes[si], action.Act)
			time.Sleep(time.Duration(sleepTimes[si]) * time.Millisecond)
			si++
		}
	}

	wg.Done()
}

// A function that runs a set of Replication Controller actions
func runRcActions(
	mgr *manager.ReplicationControllerManager,
	rcConfig ReplicationControllerConfig,
	opNum int,
	tid int) {

	rcName := mgr.GetResourceName(opNum, tid)

	actions := rcConfig.Actions

	created := false

	sleepTimes := rcConfig.SleepTimes
	si := 0

	for _, action := range actions {
		var lk, lv string
		ns := rcNamespace

		if rcConfig.Namespace != "" {
			ns = rcConfig.Namespace
		}
		actStr := strings.ToLower(action.Act)

		if actStr == manager.CREATE_ACTION {
			if created {
				log.Warningf("Only one CREATE may be executed in an operation, skipped.")
				continue
			} else {
				created = true
			}

			var spec *apiv1.ReplicationController

			createSpec := action.Spec
			updateLabelNs(createSpec, rcConfig.LabelKey, rcConfig.LabelValue, &ns, &lk, &lv)

			obj, derr := decodeYaml(createSpec.YamlSpec)
			if obj != nil && derr == nil {
				spec = obj.(*apiv1.ReplicationController)
				if spec.Kind != "ReplicationController" {
					log.Warningf("Invalid kind in yaml for ReplicationController creation: %v",
						spec.Kind)
					spec = nil
				}
			}

			if spec == nil {
				as := manager.ActionSpec{rcName, tid, opNum, ns, lk,
					lv, true, "", manager.REPLICATION_CONTROLLER}
				if createSpec.ImagePullPolicy == "Always" {
					spec = genRcSpec(createSpec.Image, createSpec.NumReplicas,
						apiv1.PullAlways, opNum, as)
				} else if createSpec.ImagePullPolicy == "Never" {
					spec = genRcSpec(createSpec.Image, createSpec.NumReplicas,
						apiv1.PullNever, opNum, as)
				} else {
					spec = genRcSpec(createSpec.Image, createSpec.NumReplicas,
						apiv1.PullIfNotPresent, opNum, as)
				}
			} else {
				// Name from yaml file are not respected to ensure integrity.
				spec.Name = rcName

				if spec.Namespace == "" {
					spec.Namespace = ns
				} else {
					ns = spec.Namespace
				}

				if spec.Spec.Selector == nil {
					spec.Spec.Selector = make(map[string]string, 0)
				}

				updateLabels(spec.Spec.Selector, rcType, opNum, tid)

				if spec.Spec.Template.ObjectMeta.Labels == nil {
					spec.Spec.Template.ObjectMeta.Labels = make(map[string]string, 0)
				}

				updateLabels(spec.Spec.Template.ObjectMeta.Labels, rcType, opNum, tid)

				if spec.Labels == nil {
					spec.Labels = make(map[string]string, 0)
				}

				updateLabels(spec.Labels, rcType, opNum, tid)
				if lk != "" && lv != "" {
					spec.Spec.Selector[lk] = lv
					spec.Spec.Template.ObjectMeta.Labels[lk] = lv
					spec.Labels[lk] = lv
				}
			}

			ae := mgr.ActionFuncs[manager.CREATE_ACTION](mgr, spec)

			if ae != nil {
				log.Error(ae)
			}
		} else if actionFunc, ok := mgr.ActionFuncs[actStr]; ok {
			lusdSpec := action.Spec
			updateLabelNs(lusdSpec, rcConfig.LabelKey, rcConfig.LabelValue, &ns, &lk, &lv)

			as := manager.ActionSpec{
				rcName, tid, opNum, ns, lk, lv,
				lusdSpec.MatchGoroutine, lusdSpec.MatchOperation, manager.REPLICATION_CONTROLLER}

			ae := actionFunc(mgr, as)

			if ae != nil {
				log.Error(ae)
			}
		}

		// TODO: add optional status checking logic here
		if si < len(sleepTimes) {
			log.Infof("Sleep %v mili-seconds after %v action", sleepTimes[si], action.Act)
			time.Sleep(time.Duration(sleepTimes[si]) * time.Millisecond)
			si++
		}
	}

	wg.Done()
}

// A function that runs a set of general resource actions
func runResourceActions(
	mgr *manager.ResourceManager,
	resConfig ResourceConfig,
	opNum int,
	tid int,
	kind string) {

	resName := mgr.GetResourceName(opNum, tid, kind)

	actions := resConfig.Actions

	created := false

	sleepTimes := resConfig.SleepTimes

	si := 0

	for _, action := range actions {
		var lk, lv string
		ns := resNamespace

		if resConfig.Namespace != "" {
			ns = resConfig.Namespace
		}
		actStr := strings.ToLower(action.Act)

		if actStr == manager.CREATE_ACTION {
			if created {
				log.Warningf("Only one CREATE may be executed in an operation, skipped.")
				continue
			} else {
				created = true
			}

			//var spec *runtime.Object
			createSpec := action.Spec
			updateLabelNs(createSpec, resConfig.LabelKey, resConfig.LabelValue, &ns, &lk, &lv)

			// TODO: Check whether YamlSpec is configured outside in resConfig (also for other types).
			obj, derr := decodeYaml(createSpec.YamlSpec)
			if obj == nil || derr != nil {
				log.Warningf("Can not decode yaml config for resource creation: %v",
					resName)
				continue
			}
			metadata, me := meta.Accessor(obj)
			if me != nil {
				log.Errorf("Failed to get metadata accessor for resource creation")
				continue
			}
			if metadata.GetNamespace() == "" {
				metadata.SetNamespace(ns)
			} else {
				ns = metadata.GetNamespace()
			}

			resLabels := make(map[string]string, 0)
			resLabels["app"] = manager.AppName

			updateLabels(resLabels, resType, opNum, tid)
			if lk != "" && lv != "" {
				resLabels[lk] = lv
			}
			metadata.SetLabels(resLabels)
			metadata.SetName(resName)
			ae := mgr.ActionFuncs[manager.CREATE_ACTION](mgr, obj)

			if ae != nil {
				log.Error(ae)
			}
		} else if actionFunc, ok := mgr.ActionFuncs[actStr]; ok {
			lusdSpec := action.Spec
			updateLabelNs(lusdSpec, resConfig.LabelKey, resConfig.LabelValue, &ns, &lk, &lv)

			as := manager.ActionSpec{
				resName, tid, opNum, ns, lk, lv,
				lusdSpec.MatchGoroutine, lusdSpec.MatchOperation, kind}
			ae := actionFunc(mgr, as)

			if ae != nil {
				log.Error(ae)
			}
		}

		// TODO: add optional status checking logic here
		if si < len(sleepTimes) {
			log.Infof("Sleep %v mili-seconds after %v action", sleepTimes[si], action.Act)
			time.Sleep(time.Duration(sleepTimes[si]) * time.Millisecond)
			si++
		}
	}

	wg.Done()
}
func waitforVmRelatedOps(
	driverVmClient ctrlClient.Client,
	resKind string,
	timeout int,
	totalWait int,
	interval int,
	opIdx int) {
	vmList := v1alpha1.VirtualMachineList{}
	for totalWait < timeout {
		err := driverVmClient.List(context.Background(), &vmList)
		if err != nil {
			panic(err)
		}
		if len(vmList.Items) > 0 {
			log.Infof("Not all %v have been deleted, "+
				"%v remaining, wait for %v mili-seconds...",
				resKind, len(vmList.Items), interval)
			time.Sleep(time.Duration(interval) * time.Millisecond)
			totalWait += interval
		} else {
			break
		}
	}
	if totalWait >= timeout {
		err := driverVmClient.List(context.Background(), &vmList)
		if err != nil {
			panic(err)
		}
		// gp := int64(0)
		// fg := metav1.DeletePropagationForeground
		if len(vmList.Items) > 0 {
			log.Infof("Timed out waiting for %v deletion, "+
				"%v remaining, force delete...",
				resKind, len(vmList.Items))
			// driverVmClient.CoreV1().Pods(pods.Items[0].Namespace).DeleteCollection(context.Background(), &metav1.DeleteOptions{
			// 	GracePeriodSeconds: &gp, PropagationPolicy: &fg}, options)
		}
	}


}
// A helper function to update labels and namespace
func waitForPodRelatedOps(
	mgrs map[string]manager.Manager,
	driverClient *kubernetes.Clientset,
	resKind string,
	lastAction string,
	timeout int,
	interval int,
	opIdx int,
	kubeConfig *restclient.Config) int {

	totalWait := 0
	var t string
	if resKind == manager.POD {
		t = podType
	} else if resKind == manager.DEPLOYMENT {
		t = depType
	} else if resKind == manager.REPLICATION_CONTROLLER {
		t = rcType
	} else if resKind == manager.STATEFUL_SET {
		t = ssType
	} else if resKind == manager.VIRTUALMACHINE {
		t = vmType
	}
	// Wait for pod action (deletion or creation)
	if strings.ToLower(lastAction) == manager.DELETE_ACTION {
		if resKind == manager.VIRTUALMACHINE {
			scheme := runtime.NewScheme()
			_ = v1alpha1.AddToScheme(scheme)

			driverVmClient, err := ctrlClient.New(kubeConfig, ctrlClient.Options{
				Scheme: scheme,
			})
			if err != nil {
				panic(err)
			}
			waitforVmRelatedOps(driverVmClient, resKind, timeout, totalWait, interval, opIdx)
		} else {
		// Wait for pod deletion
			selector := labels.Set{
				"opnum": strconv.Itoa(opIdx),
				"type":  t,
			}.AsSelector().String()
			options := metav1.ListOptions{LabelSelector: selector}
			for totalWait < timeout {
				pods, _ := driverClient.CoreV1().Pods("").List(context.Background(), options)

				if len(pods.Items) > 0 {
					log.Infof("Not all %v have been deleted, "+
						"%v remaining, wait for %v mili-seconds...",
						resKind, len(pods.Items), interval)
					time.Sleep(time.Duration(interval) * time.Millisecond)
					totalWait += interval
				} else {
					break
				}
			}
			if totalWait >= timeout {
				pods, _ := driverClient.CoreV1().Pods("").List(context.Background(), options)
				// gp := int64(0)
				// fg := metav1.DeletePropagationForeground
				if len(pods.Items) > 0 {
					log.Infof("Timed out waiting for %v deletion, "+
						"%v remaining, force delete...",
						resKind, len(pods.Items))
					// driverClient.CoreV1().Pods(pods.Items[0].Namespace).DeleteCollection(context.Background(), &metav1.DeleteOptions{
					// 	GracePeriodSeconds: &gp, PropagationPolicy: &fg}, options)
				}
			}
		}
	} else if mgr, ok := mgrs[resKind]; ok {
		var stable bool

		if strings.ToLower(lastAction) == manager.CREATE_ACTION {
			// Wait for creation
			for totalWait < timeout {
				if resKind == manager.POD {
					podMgr := mgr.(*manager.PodManager)
					stable = podMgr.IsStable()
				} else if resKind == manager.DEPLOYMENT {
					depMgr := mgr.(*manager.DeploymentManager)
					stable = depMgr.IsStable()
				} else if resKind == manager.STATEFUL_SET {
					ssMgr := mgr.(*manager.StatefulSetManager)
					stable = ssMgr.IsStable()
				} else if resKind == manager.REPLICATION_CONTROLLER {
					rcMgr := mgr.(*manager.ReplicationControllerManager)
					stable = rcMgr.IsStable()
				} else if resKind == manager.VIRTUALMACHINE {
					vmMgr := mgr.(*manager.VmManager)
					stable = vmMgr.IsStable()
				}
				if !stable {
					log.Infof("Not all %v are running, wait for %v mili-seconds...",
						resKind, interval)
					time.Sleep(time.Duration(interval) * time.Millisecond)
					totalWait += interval
				} else {
					break
				}
			}
		}
	}
	return totalWait
}

func printBoundary() {
	log.Infof("%-80v", "**********************************************************"+
		"************************************")
}
