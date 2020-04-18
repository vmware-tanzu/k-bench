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

package vmware

import ()

/* Below are structs for the VMware infrastructure part in benchmark config */

type Cidr struct {
	Address string
	Prefix  int
}

type NetworkConfig struct {
	Mode        string
	VcNetwork   string
	Gateway     string
	Start       string
	Count       int
	NetworkMask string
}

type EsxConfig struct {
	HostIp   string
	UserName string
	Password string
}

type VsphereConfig struct {
	VcUser            string
	VcPassword        string
	VcIp              string
	Cluster           string
	Hosts             []EsxConfig
	Size              string
	MasterDNS         []string
	WorkerDNS         []string
	MasterNTP         []string
	WorkerNTP         []string
	MasterNetwork     NetworkConfig
	ManagementNetwork NetworkConfig
	WorkerNetwork     NetworkConfig
	MasterStore       string // Only support using one datastore at this point
	WorkerStore       string // Only support using one datastore at this point
	PodCidr           Cidr
	ServiceCidr       Cidr
}

/* Below are structs that represent json body in wcp request */

type WcpAddressRange struct {
	AddressCount    int    `json:"address_count"`
	Gateway         string `json:"gateway"`
	StartingAddress string `json:"starting_address"`
	SubnetMask      string `json:"subnet_mask"`
}

type WcpNetwork struct {
	Range     WcpAddressRange `json:"address_range"`
	Mode      string          `json:"mode"`
	VcNetwork string          `json:"network"`
}

type WcpCidr struct {
	Address string `json:"address"`
	Prefix  int    `json:"prefix"`
}

type WcpSpec struct {
	SizeHint          string     `json:"size_hint"`
	MasterDNS         []string   `json:"master_DNS"`
	MasterNTP         []string   `json:"master_NTP_servers"`
	MasterNetwork     WcpNetwork `json:"master_cluster_network"`
	ManagementNetwork WcpNetwork `json:"master_management_network"`
	MasterStore       []string   `json:"master_storage"`
	PodCidr           WcpCidr    `json:"pod_cidr"`
	ServiceCidr       WcpCidr    `json:"service_cidr"`
	VxlanPort         int        `json:"vxlan_port"`
	WorkerDNS         []string   `json:"worker_DNS"`
	WorkerNTP         []string   `json:"worker_NTP_servers"`
	WorkerNetwork     WcpNetwork `json:"worker_cluster_network"`
	WorkerStore       []string   `json:"worker_storage"`
}

type WcpConfig struct {
	Cluster string  `json:"cluster"`
	Spec    WcpSpec `json:"spec"`
}

/* Below are structs that capture json responses from vCenter Server */

type Cluster struct {
	Id         string `json:"cluster"`
	Name       string `json:"name"`
	DrsEnabled bool   `json:"drs_enabled"`
	HaEnabled  bool   `json:"ha_enabled"`
}

type Clusters struct {
	AllClusters []Cluster `json:"value"`
}

type Datastore struct {
	Id        string `json:"datastore"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	FreeSpace int64  `json:"free_space"`
	Capacity  int64  `json:"capacity"`
}

type Datastores struct {
	AllDatastores []Datastore `json:"value"`
}

type Network struct {
	Id   string `json:"network"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type Networks struct {
	AllNetworks []Network `json:"value"`
}
