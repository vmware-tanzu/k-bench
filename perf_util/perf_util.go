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

package perf_util

import (
	log "github.com/sirupsen/logrus"
	"os"
	"strings"
	"time"
)

const wavefrontInputFileNamePrefix = "kbench-wavefront-"

func WriteDataPoints(now time.Time, points []WavefrontDataPoint, wavefrontPathDir string, prefix string) {
	//compose filename with timestamp
	timeFormat := now.Format("2006-01-02_15:04:05")
	wavefrontInputFileName := wavefrontPathDir + "/" + wavefrontInputFileNamePrefix + timeFormat + ".log"
	// fmt.Printf("Sending to wavefront ...\n")
	// Write performance metrics to telegraf file which will be processed by wavefront agent
	file, err := os.OpenFile(wavefrontInputFileName, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal(err)
	}

	var metricLines []string
	for _, point := range points {
		metricLine := point.String()
		if len(prefix) > 0 {
			metricLine = prefix + metricLine
		}
		metricLines = append(metricLines, metricLine)
		metricLines = append(metricLines, "\n")
	}
	file.WriteString(strings.Join(metricLines, ""))
	defer file.Close()
}

//Get hostname from url, for example, https://master.eng.vmware.com:8443, extract master.eng.vmware.com
func GetHostnameFromUrl(url string) string {
	urlComponents := strings.Split(url, "https://")
	hostnameAndPorts := strings.Split(urlComponents[1], ":")
	hostname := hostnameAndPorts[0]
	return hostname
}
