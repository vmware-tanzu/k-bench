# Copyright 2017 VMware, Inc. All rights reserved. -- VMware Confidential
# Description:  Perf CICD WaveFront Example
# Group-perf: optional
# Timeout: 3000

from telegraf.client import TelegrafClient
client = TelegrafClient(host='localhost', port=8094, tags={'host': 'diffhost'})
print 'Client created'

# Records a single value with one tag
client.metric('GK.testmetric', float(60), tags={'app_name_descr': 'CICD_test-app'}, timestamp=long(1528483840794000))
print 'Metric sent to Wavefront'
