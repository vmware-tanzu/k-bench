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

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http"

	log "github.com/sirupsen/logrus"
)

const (
	AUTH_URI           string = "/rest/com/vmware/cis/session"
	CLUSTER_INFO_URI   string = "/rest/vcenter/cluster"
	WCP_INFO_URI       string = "/rest/vcenter/wcp/clusters"
	DATASTORE_INFO_URI string = "/rest/vcenter/datastore"
	NETWORK_INFO_URI   string = "/rest/vcenter/network"
)

// Validate the given cluster, return the cluster id and whether it is wcp
// compatible and whether it is wcp enabled already
func validateCluster(name string,
	vcip string,
	client *http.Client,
	ck *http.Cookie) (string, bool, bool) {

	url := "https://" + vcip + CLUSTER_INFO_URI
	clusterReq, _ := http.NewRequest("GET", url, nil)
	clusterReq.AddCookie(ck)

	clusterRes, ce := client.Do(clusterReq)
	defer clusterRes.Body.Close()

	if ce != nil {
		log.Errorf("Failed to get clusters, error: %v", ce)
		return "", false, false
	}

	var clusters Clusters
	decoder := json.NewDecoder(clusterRes.Body)
	je := decoder.Decode(&clusters)

	if je != nil {
		log.Errorf("Failed to decode cluster info response, error: %v", je)
		return "", false, false
	}

	for _, cls := range clusters.AllClusters {
		if cls.Name == name {
			if cls.DrsEnabled == true && cls.HaEnabled == true {
				log.Infof("The specified cluster %v is wcp compatible.", cls.Id)
				// Futher validate whether it is wcp enabled already
				url := "https://" + vcip + WCP_INFO_URI + "/" + cls.Id
				wcpReq, _ := http.NewRequest("GET", url, nil)
				wcpReq.AddCookie(ck)

				wcpRes, we := client.Do(wcpReq)
				if we != nil {
					log.Errorf("Failed to retrieve wcp info for the cluster, "+
						"treating as wcp not enabled. Error:", we)
					return cls.Id, true, false
				}

				if wcpRes.StatusCode == http.StatusOK {
					log.Warnf("Cluster %v already enabled wcp", cls.Id)
					return cls.Id, true, true
				} else {
					log.Infof("Cluster %v didn't enable wcp", cls.Id)
					return cls.Id, true, false
				}
			} else {
				log.Errorf("Cluster %v is not wcp compatible.", cls.Id)
				return cls.Id, false, false
			}
		}
	}

	// When cluster is not found
	return "", false, false
}

func getDatastoreId(name string,
	vcip string,
	client *http.Client,
	ck *http.Cookie) string {

	url := "https://" + vcip + DATASTORE_INFO_URI
	req, _ := http.NewRequest("GET", url, nil)
	req.AddCookie(ck)

	res, de := client.Do(req)
	defer res.Body.Close()

	if de != nil {
		log.Errorf("Failed to get datastore info, error: %v", de)
		return ""
	}

	var datastores Datastores
	decoder := json.NewDecoder(res.Body)
	je := decoder.Decode(&datastores)

	if je != nil {
		log.Errorf("Failed to decode datastore info response, error: %v", je)
		return ""
	}

	for _, store := range datastores.AllDatastores {
		if store.Name == name {
			return store.Id
		}
	}

	return ""
}

func getNetworkId(name string,
	vcip string,
	client *http.Client,
	ck *http.Cookie) string {

	url := "https://" + vcip + NETWORK_INFO_URI
	req, _ := http.NewRequest("GET", url, nil)
	req.AddCookie(ck)

	res, ne := client.Do(req)
	defer res.Body.Close()

	if ne != nil {
		log.Errorf("Failed to get network info, error: %v", ne)
		return ""
	}

	var networks Networks
	decoder := json.NewDecoder(res.Body)
	je := decoder.Decode(&networks)

	if je != nil {
		log.Errorf("Failed to decode network info response, error: %v", je)
		return ""
	}

	for _, network := range networks.AllNetworks {
		if network.Name == name {
			return network.Id
		}
	}

	return ""
}

func EnableWcp(cfg VsphereConfig) bool {
	vcip := cfg.VcIp
	url := "https://" + vcip + AUTH_URI
	req, _ := http.NewRequest("POST", url, nil)
	req.SetBasicAuth("Administrator@vsphere.local", "Admin!23")

	transCfg := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	restClient := &http.Client{Transport: transCfg}
	res, err := restClient.Do(req)

	if err != nil || len(res.Cookies()) == 0 {
		log.Errorf("Failed to get authentication info, error: %v", err)
		return false
	}

	clusterId, compatible, enabled := validateCluster(cfg.Cluster, vcip,
		restClient, res.Cookies()[0])

	if clusterId == "" {
		log.Errorf("Cluster %v not found", cfg.Cluster)
		return false
	}

	masterDatastore := getDatastoreId(cfg.MasterStore, vcip, restClient,
		res.Cookies()[0])

	workerDatastore := getDatastoreId(cfg.WorkerStore, vcip, restClient,
		res.Cookies()[0])

	masterNetwork := getNetworkId(cfg.MasterNetwork.VcNetwork, vcip, restClient,
		res.Cookies()[0])

	managementNetwork := getNetworkId(cfg.ManagementNetwork.VcNetwork, vcip,
		restClient, res.Cookies()[0])

	workerNetwork := getNetworkId(cfg.WorkerNetwork.VcNetwork, vcip, restClient,
		res.Cookies()[0])

	if masterDatastore == "" || workerDatastore == "" {
		log.Errorf("One of the specified datastores is not found")
		return false
	}

	if masterNetwork == "" || managementNetwork == "" || workerNetwork == "" {
		log.Errorf("One of the specified network is not found")
		return false
	}

	masterDsArr := []string{masterDatastore}
	workerDsArr := []string{workerDatastore}

	wc := WcpConfig{
		Cluster: clusterId,
		Spec: WcpSpec{
			SizeHint:  "LARGE",
			MasterDNS: cfg.MasterDNS,
			WorkerDNS: cfg.WorkerDNS,
			MasterNTP: cfg.MasterNTP,
			WorkerNTP: cfg.WorkerNTP,
			MasterNetwork: WcpNetwork{
				Range: WcpAddressRange{
					AddressCount:    cfg.MasterNetwork.Count,
					Gateway:         cfg.MasterNetwork.Gateway,
					StartingAddress: cfg.MasterNetwork.Start,
					SubnetMask:      cfg.MasterNetwork.NetworkMask,
				},
				Mode:      cfg.MasterNetwork.Mode,
				VcNetwork: masterNetwork,
			},
			ManagementNetwork: WcpNetwork{
				Range: WcpAddressRange{
					AddressCount:    cfg.ManagementNetwork.Count,
					Gateway:         cfg.ManagementNetwork.Gateway,
					StartingAddress: cfg.ManagementNetwork.Start,
					SubnetMask:      cfg.ManagementNetwork.NetworkMask,
				},
				Mode:      cfg.ManagementNetwork.Mode,
				VcNetwork: managementNetwork,
			},
			WorkerNetwork: WcpNetwork{
				Range: WcpAddressRange{
					AddressCount:    cfg.WorkerNetwork.Count,
					Gateway:         cfg.WorkerNetwork.Gateway,
					StartingAddress: cfg.WorkerNetwork.Start,
					SubnetMask:      cfg.WorkerNetwork.NetworkMask,
				},
				Mode:      cfg.WorkerNetwork.Mode,
				VcNetwork: workerNetwork,
			},
			MasterStore: masterDsArr,
			WorkerStore: workerDsArr,
			PodCidr: WcpCidr{
				Address: cfg.PodCidr.Address,
				Prefix:  cfg.PodCidr.Prefix,
			},
			ServiceCidr: WcpCidr{
				Address: cfg.ServiceCidr.Address,
				Prefix:  cfg.ServiceCidr.Prefix,
			},
			VxlanPort: 4789, // TODO: for now hard coded, make this configurable
		},
	}

	js, _ := json.Marshal(wc)

	if compatible && !enabled {
		enableUrl := "https://" + vcip + WCP_INFO_URI + "/" + clusterId
		enableReq, _ := http.NewRequest("POST", enableUrl, bytes.NewBuffer(js))

		q := enableReq.URL.Query()
		q.Add("action", "enable")

		enableReq.URL.RawQuery = q.Encode()

		enableReq.AddCookie(res.Cookies()[0])
		enableReq.Header.Set("Content-Type", "application/json")

		log.Infof("Sending WCP enable request using data: %v", string(js))

		enableRes, ee := restClient.Do(enableReq)

		if ee != nil {
			log.Errorf("Failed to enable WCP cluster, error: %v", ee)
			return false
		}

		if enableRes.StatusCode != http.StatusOK {
			log.Infof("Failed to enable WCP cluster, status code: %v", enableRes.StatusCode)
		} else {
			log.Infof("Successfully sent request to enable WCP cluster. " +
				"Please wait for the cluster to be ready...")
		}

		res.Body.Close()
		enableRes.Body.Close()
	}

	return false

}
