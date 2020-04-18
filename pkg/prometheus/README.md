# Prometheus Integration
This directory contains coreos' prometheus for monitoring kubernetes clusters using 
Prometheus Operators. To setup and enable prometheus monitoring before running your
workload, include the below _PrometheusManifestPaths_ configuration option in your
benchmark config file: 

```
        "PrometheusManifestPaths": [
                "pkg/prometheus/manifests/setup",
                "pkg/prometheus/manifests"
        ],
```

For general instructions and more details on kube-prometheus, please check: 
[kube-prometheus](https://github.com/coreos/kube-prometheus).
