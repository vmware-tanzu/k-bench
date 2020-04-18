kubectl delete all --all -n kbench-pod-namespace;
kubectl delete pvc fio-block-pvc -n kbench-pod-namespace;
kubectl delete all --all -n kbench-deployment-namespace;
kubectl delete all --all -n kbench-service-namespace;
kubectl delete all --all -n kbench-rc-namespace;
kubectl delete all --all -n kbench-resource-namespace;
