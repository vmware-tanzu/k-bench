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
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	//"k8s.io/client-go/kubernetes"
	"k-bench/perf_util"
	"time"
)

// Supported actions
const (
	CREATE_ACTION string = "create"
	DELETE_ACTION string = "delete"
	LIST_ACTION   string = "list"
	GET_ACTION    string = "get"
	UPDATE_ACTION string = "update"
	SCALE_ACTION  string = "scale"
	RUN_ACTION    string = "run"
	COPY_ACTION   string = "copy"
)

// Supported k8s resource kinds
const (
	POD                     string = "Pod"
	//VIRTUALMACHINE          string = "Vm"
	DEPLOYMENT              string = "Deployment"
	STATEFUL_SET            string = "StatefulSet"
	REPLICATION_CONTROLLER  string = "ReplicationController"
	SERVICE                 string = "Service"
	NAMESPACE               string = "Namespace"
	CONFIG_MAP              string = "ConfigMap"
	ENDPOINTS               string = "Endpoints"
	EVENT                   string = "Event"
	COMPONENT_STATUS        string = "ComponentStatus"
	NODE                    string = "Node"
	LIMIT_RANGE             string = "LimitRange"
	PERSISTENT_VOLUME_CLAIM string = "PersistentVolumeClaim"
	PERSISTENT_VOLUME       string = "PersistentVolume"
	POD_TEMPLATE            string = "PodTemplate"
	RESOURCE_QUOTA          string = "ResourceQuota"
	SECRET                  string = "Secret"
	SERVICE_ACCOUNT         string = "ServiceAccount"
	ROLE                    string = "Role"
	ROLE_BINDING            string = "RoleBinding"
	CLUSTER_ROLE            string = "ClusterRole"
	CLUSTER_ROLE_BINDING    string = "ClusterRoleBinding"
)

const (
	ALL_OPERATION  string = "all"
	CURR_OPERATION string = "current"
)

var PodRelatedResources = map[string]bool{
	POD:                    true,
	DEPLOYMENT:             true,
	REPLICATION_CONTROLLER: true,
	STATEFUL_SET:           true,
}

const AppName string = "kbench"

type Manager interface {
	// Create the specified resource
	Create(spec interface{}) error

	// Delete the specified resource
	Delete(name interface{}) error

	// Calculate metrics
	CalculateStats()

	// Log stats for the manager
	LogStats()

	SendMetricToWavefront(now time.Time, wfTags []perf_util.WavefrontTag, wavefrontPathDir string, prefix string)

	// Delete all the resources created by this manager
	DeleteAll() error

	// TODO: add a method to report stats

	CalculateSuccessRate() int
}

type NewManagerFunc func() Manager

var Managers = map[string]NewManagerFunc{
	"Pod":                   NewPodManager,
	//"Vm":					 NewVmManager,
	"Deployment":            NewDeploymentManager,
	"StatefulSet":           NewStatefulSetManager,
	"Namespace":             NewNamespaceManager,
	"Service":               NewServiceManager,
	"ReplicationController": NewReplicationControllerManager,
	"Resource":              NewResourceManager,
}

type ActionSpec struct {
	Name           string
	Tid            int
	Oid            int
	Namespace      string
	LabelKey       string
	LabelValue     string
	MatchGoroutine bool
	MatchOperation string
	Kind           string
}

type RunSpec struct {
	RunCommand   string
	ActionFilter ActionSpec
}

type CopySpec struct {
	ParentOutDir  string
	LocalPath     string
	ContainerPath string
	Upload        bool
	ActionFilter  ActionSpec
}

func GetListOptions(s ActionSpec) metav1.ListOptions {
	filters := make(map[string]string, 0)
	if s.LabelKey != "" && s.LabelValue != "" {
		filters[s.LabelKey] = s.LabelValue
	}
	if s.MatchGoroutine == true {
		filters["tid"] = strconv.Itoa(s.Tid)
	}
	if strings.ToLower(s.MatchOperation) == CURR_OPERATION {
		filters["opnum"] = strconv.Itoa(s.Oid)
	} else if strings.ToLower(s.MatchOperation) == ALL_OPERATION {
		filters["app"] = AppName
	}
	if len(filters) == 0 {
		selector := fields.Set{"metadata.name": s.Name}.AsSelector().String()
		options := metav1.ListOptions{FieldSelector: selector}
		return options
	} else {
		selector := labels.Set(filters).AsSelector().String()
		options := metav1.ListOptions{LabelSelector: selector}
		return options
	}
}

func GetManager(managerName string) (Manager, error) {
	newManagerFn, ok := Managers[managerName]

	if !ok {
		log.Errorf("%s manager not found.", managerName)
		return nil, fmt.Errorf("No such provider: %s", managerName)
	}

	log.Infof("Created a new %s manager.", managerName)
	return newManagerFn(), nil
}
