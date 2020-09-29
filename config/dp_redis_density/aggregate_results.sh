#/bin/bash
Num_Instances=25;

Agg_throughput=0;
for((num=0;num < ${Num_Instances};num++))
{ 
	throughput=`cat */kbench-pod-oid-0-tid-${num}/memtier.out  | grep "Totals" | awk {'print $2'}`
	echo "Throughput of pod $num is $throughput";
	Agg_throughput=`echo "$throughput + $Agg_throughput" | bc`
}
echo "Aggregate throughput = $Agg_throughput";
