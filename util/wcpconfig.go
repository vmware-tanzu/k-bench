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
	"k-bench/infrastructure/vmware"
	"k-bench/perf_util"
)

type CreateSpec struct {
	Namespace string
	YamlSpec  string
}

type ImageSpec struct {
	Image           string
	ImagePullPolicy string // Always, IfNotPresent, or Never
}

type FilterSpec struct {
	LabelKey       string
	LabelValue     string
	MatchGoroutine bool
	MatchOperation string
}

type BasicCreateSpec struct {
	CreateSpec
	LabelKey   string
	LabelValue string
}

type PodCreateSpec struct {
	CreateSpec
	ImageSpec
	LabelKey   string
	LabelValue string
}

type DeploymentCreateSpec struct {
	CreateSpec
	ImageSpec
	LabelKey    string
	LabelValue  string
	NumReplicas int32
}

type RcCreateSpec struct {
	CreateSpec
	ImageSpec
	LabelKey    string
	LabelValue  string
	NumReplicas int32
}

type CopySpec struct {
	LocalPath     string
	ContainerPath string
	Upload        bool
}

type RunSpec struct {
	Command string
}

type LUSDSpec struct {
	Namespace string
	FilterSpec
}

type ActionSpec struct {
	CreateSpec
	ImageSpec
	FilterSpec
	CopySpec
	RunSpec
	NumReplicas int32
}

type Action struct {
	Act  string
	Spec ActionSpec
}

type PredicateSpec struct {
	// Resources in PredicateSpec has two possible formats:
	// 1. namespace/kind/[object_name/][container_name]
	// 2. group/version/namespaces/namespace/kind/[object_name/][container_name]
	// An implicit condition by specifying this is that the resources must exist
	// If container_name is specified, the corresponding resource has to be pods in Running phase
	Resource string
	Labels string // label_key_name=label_value_name;... TODO: add build-in vars to refer to current oid and tid
	Command  string // Command to execute (inside the container if container_name above is specified)
	Expect   string // contains:string or !contains:string
}

type DeploymentConfig struct {
	Namespace       string
	NumReplicas     int32
	Image           string
	ImagePullPolicy string   // Always, IfNotPresent, or Never
	Count           int      // Number of deployment actions to run in parallel (concurrency)
	Actions         []Action // CREATE, GET, SCALE, UPDATE, LIST, or DELETE
	SleepTimes      []int    // Sleep time in ms after each action
	YamlSpec        string
	// Below are used for pod filtering (decouple LIST from CREATE)
	FilterSpec
}

type StatefulSetConfig struct {
	DeploymentConfig // Currently StatefulSetConfig shares the same structure with DeploymentConfig
}

type PodConfig struct {
	Namespace           string
	Image               string   // Image to use for pod creation
	Count               int      // Number of pod actions to run in parallel (concurrency)
	ContainerNamePrefix string   // A name prefix for containers inside the Pod
	ImagePullPolicy     string   // Always, IfNotPresent, or Never
	Actions             []Action // CREATE, GET, UPDATE, RUN, LIST, or DELETE
	SleepTimes          []int    // Sleep time in ms after each action
	Command             string   // Command to be run for RUN action
	PodNamePrefix       string   // A prefix for a set of pods in an operation
	YamlSpec            string   // Path for yaml file used for pod creation
	// Below are used for pod filtering (for mutable actions)
	FilterSpec
	// Used for COPY action tp copy file(s) from and to pod container(s)
	FileSpec CopySpec
}

type NamespaceConfig struct {
	Count      int
	Actions    []Action // CREATE, GET, UPDATE, LIST, or DELETE
	SleepTimes []int    // Sleep time in ms after each action
	FilterSpec
}

type ServiceConfig struct {
	Namespace  string
	Count      int
	Actions    []Action // CREATE, GET, UPDATE, LIST, or DELETE
	SleepTimes []int    // Sleep time in ms after each action
	YamlSpec   string
	FilterSpec
}

type ReplicationControllerConfig struct {
	Namespace       string
	NumReplicas     int32
	Image           string
	ImagePullPolicy string // Always, IfNotPresent, or Never
	Count           int
	Actions         []Action // CREATE, SCALE, UPDATE, LIST, or DELETE
	SleepTimes      []int    // Sleep time in ms after each action
	YamlSpec        string
	FilterSpec
}

type ResourceConfig struct {
	Namespace  string
	Count      int
	Actions    []Action
	SleepTimes []int
	YamlSpec   string
	FilterSpec
}

type WcpOp struct {
	Predicate             PredicateSpec
	Pod                   PodConfig                   `json:"Pods"`
	Deployment            DeploymentConfig            `json:"Deployments"`
	StatefulSet           StatefulSetConfig           `json:"StatefulSets"`
	Namespace             NamespaceConfig             `json:"Namespaces"`
	Service               ServiceConfig               `json:"Services"`
	ReplicationController ReplicationControllerConfig `json:"ReplicationControllers"`
	ConfigMap             ResourceConfig              `json:"ConfigMaps"`
	Endpoints             ResourceConfig              `json:"Endpoints"`
	Event                 ResourceConfig              `json:"Events"`
	ComponentStatus       ResourceConfig              `json:"ComponentStatuses"`
	Node                  ResourceConfig              `json:"Nodes"`
	LimitRange            ResourceConfig              `json:"LimitRanges"`
	PersistentVolumeClaim ResourceConfig              `json:"PersistentVolumeClaims"`
	PersistentVolume      ResourceConfig              `json:"PersistentVolumes"`
	PodTemplate           ResourceConfig              `json:"PodTemplates"`
	ResourceQuota         ResourceConfig              `json:"ResourceQuotas"`
	Secret                ResourceConfig              `json:"Secrets"`
	ServiceAccount        ResourceConfig              `json:"ServiceAccounts"`
	Role                  ResourceConfig              `json:"Roles"`
	RoleBinding           ResourceConfig              `json:"RoleBindings"`
	ClusterRole           ResourceConfig              `json:"ClusterRoles"`
	ClusterRoleBinding    ResourceConfig              `json:"ClusterRoleBindings"`
	RepeatTimes           int
}

type TestConfig struct {
	VsphereInfra vmware.VsphereConfig
	// TODO: add other infrastructure support
	BlockingLevel           string
	Timeout                 int
	CheckingInterval        int
	Cleanup                 bool
	Operations              []WcpOp
	Tags                    []perf_util.WavefrontTag
	WavefrontPathDir        string
	PrometheusManifestPaths []string
	SleepTimeAfterRun       int
	DockerCompose           string
	RuntimeInMinutes        int
}
