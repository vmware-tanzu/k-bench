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
	"k-bench/manager"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"github.com/vmware-tanzu/vm-operator-api/api/v1alpha1"
	v1alpha1_tkg "gitlab.eng.vmware.com/core-build/guest-cluster-controller/apis/run.tanzu/v1alpha1"
)

func genPodSpec(image string,
	containerPrefix string,
	ipp apiv1.PullPolicy,
	on int,
	as manager.ActionSpec) *apiv1.Pod {
	labels := map[string]string{
		"app":   manager.AppName,
		"type":  podType,
		"opnum": strconv.Itoa(on),
		"tid":   strconv.Itoa(as.Tid),
	}
	if as.LabelKey != "" && as.LabelValue != "" {
		labels[as.LabelKey] = as.LabelValue
	}
	spec := &apiv1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: as.Namespace,
			Name:      as.Name,
			Labels:    labels,
		},
		Spec: apiv1.PodSpec{
			Containers: []apiv1.Container{
				{
					Name:            containerPrefix + as.Name,
					Image:           image,
					ImagePullPolicy: ipp,
					Ports: []apiv1.ContainerPort{
						{
							Name:          "http",
							Protocol:      apiv1.ProtocolTCP,
							ContainerPort: 80,
						},
					},
				},
			},
		},
	}

	return spec
}

func genDeploymentSpec(image string,
	replica int32,
	ipp apiv1.PullPolicy,
	on int,
	as manager.ActionSpec) *appsv1.Deployment {

	labels := map[string]string{
		"app":   manager.AppName,
		"type":  depType,
		"opnum": strconv.Itoa(on),
		"tid":   strconv.Itoa(as.Tid),
	}
	if as.LabelKey != "" && as.LabelValue != "" {
		labels[as.LabelKey] = as.LabelValue
	}

	podSpec := apiv1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			//Name: name,
			Namespace: as.Namespace,
			Labels:    labels,
		},
		Spec: apiv1.PodSpec{
			Containers: []apiv1.Container{
				{
					Name:            as.Name,
					Image:           image,
					ImagePullPolicy: ipp,
					Ports: []apiv1.ContainerPort{
						{
							Name:          "http",
							Protocol:      apiv1.ProtocolTCP,
							ContainerPort: 80,
						},
					},
				},
			},
		},
	}

	mlabels := map[string]string{
		"app":   manager.AppName,
		"opnum": strconv.Itoa(on),
		"tid":   strconv.Itoa(as.Tid),
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      as.Name,
			Namespace: as.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replica,
			Selector: &metav1.LabelSelector{
				MatchLabels: mlabels,
			},
			Template: podSpec,
		},
	}

	return deployment
}

func genStatefulSetSpec(image string,
	replica int32,
	ipp apiv1.PullPolicy,
	on int,
	as manager.ActionSpec) *appsv1.StatefulSet {

	labels := map[string]string{
		"app":   manager.AppName,
		"type":  ssType,
		"opnum": strconv.Itoa(on),
		"tid":   strconv.Itoa(as.Tid),
	}
	if as.LabelKey != "" && as.LabelValue != "" {
		labels[as.LabelKey] = as.LabelValue
	}

	podSpec := apiv1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			//Name: name,
			Namespace: as.Namespace,
			Labels:    labels,
		},
		Spec: apiv1.PodSpec{
			Containers: []apiv1.Container{
				{
					Name:            as.Name,
					Image:           image,
					ImagePullPolicy: ipp,
					Ports: []apiv1.ContainerPort{
						{
							Name:          "http",
							Protocol:      apiv1.ProtocolTCP,
							ContainerPort: 80,
						},
					},
				},
			},
		},
	}

	mlabels := map[string]string{
		"app":   manager.AppName,
		"opnum": strconv.Itoa(on),
		"tid":   strconv.Itoa(as.Tid),
	}

	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      as.Name,
			Namespace: as.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replica,
			Selector: &metav1.LabelSelector{
				MatchLabels: mlabels,
			},
			Template: podSpec,
		},
	}

	return statefulset
}

func genNamespaceSpec(name string, on int, tid int) *apiv1.Namespace {
	spec := &apiv1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"app":   manager.AppName,
				"type":  nsType,
				"opnum": strconv.Itoa(on),
				"tid":   strconv.Itoa(tid),
			},
		},
		Spec: apiv1.NamespaceSpec{},
	}

	return spec
}

func genServiceSpec(on int, as manager.ActionSpec) *apiv1.Service {
	labels := map[string]string{
		"app":   manager.AppName,
		"type":  svcType,
		"opnum": strconv.Itoa(on),
		"tid":   strconv.Itoa(as.Tid),
	}
	if as.LabelKey != "" && as.LabelValue != "" {
		labels[as.LabelKey] = as.LabelValue
	}

	spec := &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      as.Name,
			Namespace: as.Namespace,
			Labels:    labels,
		},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{
				{
					Protocol: apiv1.ProtocolTCP,
					Port:     80,
				},
			},
			Selector: map[string]string{
				"app": manager.AppName,
			},
		},
	}
	return spec
}

func genRcSpec(image string,
	replica int32,
	ipp apiv1.PullPolicy,
	on int,
	as manager.ActionSpec) *apiv1.ReplicationController {

	labels := map[string]string{
		"app":   manager.AppName,
		"type":  rcType,
		"opnum": strconv.Itoa(on),
		"tid":   strconv.Itoa(as.Tid),
	}
	if as.LabelKey != "" && as.LabelValue != "" {
		labels[as.LabelKey] = as.LabelValue
	}

	podSpec := apiv1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			//Name: name,
			Namespace: as.Namespace,
			Labels:    labels,
		},
		Spec: apiv1.PodSpec{
			Containers: []apiv1.Container{
				{
					Name:            as.Name,
					Image:           image,
					ImagePullPolicy: ipp,
					Ports: []apiv1.ContainerPort{
						{
							Name:          "http",
							Protocol:      apiv1.ProtocolTCP,
							ContainerPort: 80,
						},
					},
				},
			},
		},
	}

	slabels := map[string]string{
		"app":   manager.AppName,
		"opnum": strconv.Itoa(on),
		"tid":   strconv.Itoa(as.Tid),
	}

	rc := &apiv1.ReplicationController{
		ObjectMeta: metav1.ObjectMeta{
			Name:      as.Name,
			Namespace: as.Namespace,
			Labels:    labels,
		},
		Spec: apiv1.ReplicationControllerSpec{
			Replicas: &replica,
			Selector: slabels,
			Template: &podSpec,
		},
	}

	return rc
}

func genVmSpec(classname string,
	image string,
	storageclass string,
	powerstate string,
	on int,
	as manager.ActionSpec) *v1alpha1.VirtualMachine {
	labels := map[string]string{
		"app":   manager.AppName,
		"type":  vmType,
		"opnum": strconv.Itoa(on),
		"tid":   strconv.Itoa(as.Tid),
	}
	if as.LabelKey != "" && as.LabelValue != "" {
		labels[as.LabelKey] = as.LabelValue
	}
	spec := &v1alpha1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: as.Namespace,
			Name:      as.Name,
			Labels:    labels,
		},
		Spec: v1alpha1.VirtualMachineSpec{
			ClassName: classname,
			ImageName: image, //"ubuntu-20-04-vmservice-v1alpha1-20210528-ovf",
			StorageClass: storageclass,
			PowerState: "poweredOn",
		},
	}
	return spec
}

func genTkgSpec(
	ControlPlane_Count int32, ControlPlane_Class string, ControlPlane_StorageClass string,
	Workers_Count int32, Workers_Class string, Workers_StorageClass string,
	Distribution_Version string,
	Network_CNI_Name string, Network_Services_CIDRBlocks []string, Network_Pods_CIDRBlocks []string,
	Network_ServiceDomain string,
	on int,
	as manager.ActionSpec) *v1alpha1_tkg.TanzuKubernetesCluster {
	labels := map[string]string{
		"app":   manager.AppName,
		"type":  tkgType,
		"opnum": strconv.Itoa(on),
		"tid":   strconv.Itoa(as.Tid),
	}
	if as.LabelKey != "" && as.LabelValue != "" {
		labels[as.LabelKey] = as.LabelValue
	}
	spec := &v1alpha1_tkg.TanzuKubernetesCluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: as.Namespace,
			Name:      as.Name,
			Labels:    labels,
		},
		Spec: v1alpha1_tkg.TanzuKubernetesClusterSpec{
			Topology: v1alpha1_tkg.Topology{
				ControlPlane: v1alpha1_tkg.TopologySettings{
					Count:        	ControlPlane_Count,
					Class:        	ControlPlane_Class,
					StorageClass: 	ControlPlane_StorageClass,
				},
				Workers: v1alpha1_tkg.TopologySettings{
					Count: 			Workers_Count,
					Class: 			Workers_Class,
					StorageClass: 	Workers_StorageClass,
				},
			},
			Distribution: v1alpha1_tkg.Distribution{
				Version: Distribution_Version, //"v1.20.7+vmware.1-tkg.1.7fb9067",
			},
			Settings: &v1alpha1_tkg.Settings{
				Network: &v1alpha1_tkg.Network{
					CNI: &v1alpha1_tkg.CNIConfiguration{
						Name: Network_CNI_Name,
					},
					Services: &v1alpha1_tkg.NetworkRanges{
						CIDRBlocks: Network_Services_CIDRBlocks,
					},
					Pods: &v1alpha1_tkg.NetworkRanges{
						CIDRBlocks: Network_Pods_CIDRBlocks,
					},
					ServiceDomain: Network_ServiceDomain,
				},
			},
		},
	}
	return spec
}