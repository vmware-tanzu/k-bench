#!/bin/bash

# Use the following to turn the stats for a particular experiment and control the frequency of collection
ESXTOP_ALL=1; ESXTOP_ALL_SLEEPTIME="1s"; ESXTOP_SKIP_INTERRUPTS=1; #Minimum sampling time when not skipping interrupts is 5 minutes, but skipping interrupt cookies gives a sampling time of 1 minute 
ESXTOP_SKIP_CPU=0; ESXTOP_SKIP_MEMORY=0; #Skipping CPU and memory stats does not help with sampling frequency, but can help reduce the network congetion going out of the client

ESXTOP_CPUSUMMARY=1; ESXTOP_CPUSUMMARY_SLEEPTIME="1m";

MEMSTATS=1; MEMSTATS_SLEEPTIME="30s";
SCHEDSTATS=0; SCHEDSTATS_SLEEPTIME="1m";
WAVECOUNTER=0; #Ensure you have the desired set of hardware counter events in golden/ESX/WC/EVENTS file. You can use golden/ESX/WC/Wavecounter.py to explore the counters on a host and how to formulate this EVENTS file.  
ESX_DISKSPACE=0; ESX_DISKSPACE_SLEEPTIME="1m";

# DEBUG_MODE prints data sent to wavefront to stdout for debugging and leaves ssh with default log level instead of quite
DEBUG_MODE=0;
WRITE_MEMSTATS_TO_DISK=0;

usage () {
	echo "Usage: $0 -r <run_tag> -i <Host_IP_String> -w <Wavefront_source> [-o <output_folder> -k <ssh_key_file> -p <host_passwd>]";
	echo "Defaults to /tmp for output folder and a host password of null" 
	exit;
}

# Use this section for one time host settings, info collection and initiations
pre_run() 
{
	export SSHPASS=$HOSTPASS;
	for((num=0;num < $tot_hosts;num++))
	{
		# One time host settings
		#vsistatus=`$SSHCMD root@${host_ip_arr[$num]} "vsish -e set /config/Mem/intOpts/ShareVmkEnable 0"`

		# One time host info collection
		# TBD - Ideally, get the whole VSI dump. Yet to find how to do this.
		$SSHCMD root@${host_ip_arr[$num]} "vmware -vl" >> $folder/esx_${host_ip_arr[$num]}_version;
		$SSHCMD root@${host_ip_arr[$num]} "vsish -e get /hardware/cpu/cpuList/0" >> $folder/esx_${host_ip_arr[$num]}_PatchState;
		$SSHCMD root@${host_ip_arr[$num]} "vsish -e get /config/Mem/intOpts/ShareVmkEnable" >> $folder/esx_${host_ip_arr[$num]}_vmkshare_status;
	
		#Transfer any files
		if [ $ESXTOP_CPUSUMMARY -eq 1 ]; then
			if $SSHCMD root@${host_ip_arr[$num]} stat "/tmp/esxtop_cpu.sh" \> /dev/null 2\>\&1
        		then
				echo "Esxtop_cpu.sh already installed on Host" >> $folder/esx_${host_ip_arr[$num]}_esxtop_cpu;
			else
				$SCPCMD $dir/../golden/ESX/esxtop_cpu.sh root@${host_ip_arr[$num]}:/tmp/;
			fi
		fi
		
			
		#One time host initiations
		if [ $WAVECOUNTER -eq 1 ]; then
			echo "Invoking Wavecounter to start collecting";
			#echo "Commandline; python ./wavecounter.py -f EVENTS -h ${host_ip_arr[$num]} -p $HOSTPASS -r $runtag -w $WFsource -s  > $folder/WC_${host_ip_arr[$num]}.wcout 2> $folder/WC_${host_ip_arr[$num]}.wcerr"
			python $dir/../golden/ESX/WC/WaveCounter.py -f $dir/../golden/ESX/WC/EVENTS -h "${host_ip_arr[$num]}" -p "$HOSTPASS" -r "$runtag" -w "$WFsource" -s > $folder/WC_${host_ip_arr[$num]}.wcout 2> $folder/WC_${host_ip_arr[$num]}.wcerr &
			WCPID[${host_ip_arr[$num]}]=$!;
			sleep 5s;
			WCPID2[${host_ip_arr[$num]}]=`ps -elf | grep "python" | grep "WaveCounter" | grep "${host_ip_arr[$num]}" | awk {'print $4'}`
		fi
	}
}

# Use this section to reset settings on host, transfer needed files and cleanup
post_run() 
{
	export SSHPASS=$HOSTPASS;
	for((num=0;num < $tot_hosts;num++))
	{
		#Reset one time settings
		echo "Ramping down Host monitoring for host ${host_ip_arr[$num]}"
		#vsistatus=`$SSHCMD root@${host_ip_arr[$num]} "vsish -e set /config/Mem/intOpts/ShareVmkEnable 1"`

		#Transfer any files
		echo "Transfering files from host ${host_ip_arr[$num]}"
		#$SSHCMD root@${host_ip_arr[$num]} "vm-support -s" > "$folder/vm-support.tgz";
		#$SSHCMD root@${host_ip_arr[$num]} "tar -zcf /vmfs/volumes/LocalDisk/vmkperf/results.tar.gz /vmfs/volumes/LocalDisk/vmkperf/results/*"
		#$SCPCMD root@${host_ip_arr[$num]}:/vmfs/volumes/LocalDisk/vmkperf/results.tar.gz  $folder/vmkperf_stats.tgz

		#Cleanup
		echo "Cleaning up on the host ${host_ip_arr[$num]}"
		#$SSHCMD root@${host_ip_arr[$num]} "rm -rf /vmfs/volumes/LocalDisk/vmkperf/results/*"
		
		echo "Stopping initiated host monitoring for host ${host_ip_arr[$num]}"
		if [ $WAVECOUNTER -eq 1 ]; then
			if [ "${WCPID[${host_ip_arr[$num]}]}" != "" ]; then
				kill -SIGINT ${WCPID[${host_ip_arr[$num]}]};
				kill -SIGINT ${WCPID2[${host_ip_arr[$num]}]};
			fi
		fi
	}
	kill $(jobs -p) > /dev/null 2>&1
	echo "Host monitoring script terminating";
	exit;
}

#arg1 - ESXtop output as a String
esxtop_process()
{
	local esxtop_op="$1";
	local esxtop_category="ALL"
	local esxtop_max="$2"
	local host_ip=$3;
	local etop_header=`echo "$esxtop_op" | head -n 1`; local etop_body=`echo "$esxtop_op" | tail -n 1`;
	local h_arr=""; local b_arr="";

	IFS=',' read -r -a h_arr <<<"${etop_header}"; IFS=',' read -r -a b_arr <<<"${etop_body}";
	local Nelems=$((${#h_arr[@]}-1))
	local Str=''; local entity_running='';
	#echo "ESXTOP_PROCESS: Nelems is $Nelems, esxtop_category is $esxtop_category, esxtop_max is $esxtop_max";
	if [ "${esxtop_max}" != "0" ] && [ ${esxtop_max} -lt $Nelems ]; then
		Nelems=${esxtop_max}
	fi
	local iter=0;
	for((iter=1;iter <= $Nelems;iter++))
	{
		IFS='\' read -r -a entitymetricarr <<<"${h_arr[$iter]}";
                entity=${entitymetricarr[3]}; entity="${entity//\(/-}"; entity="${entity// /-}"; entity="${entity//\)/}";
		if [ ${ESXTOP_SKIP_INTERRUPTS} == 1 ] || [ ${ESXTOP_SKIP_CPU} == 1 ] || [ ${ESXTOP_SKIP_MEMORY} == 1 ]; then
			case "$entity" in
    				*Interrupt-Cookie* ) 
					if [ ${ESXTOP_SKIP_INTERRUPTS} == 1 ]; then
						continue;
					fi
				;;
    				Vcpu-* ) 
					if [ ${ESXTOP_SKIP_CPU} == 1 ]; then
						continue;
					fi
				;;
    				Group-Cpu-* ) 
					if [ ${ESXTOP_SKIP_CPU} == 1 ]; then
						continue;
					fi
				;;
    				Group-Memory-* ) 
					if [ ${ESXTOP_SKIP_MEMORY} == 1 ]; then
						continue;
					fi
				;;
   			esac 
		fi
                
                metric=${entitymetricarr[4]}; metric="${metric// /-}"; metric="${metric//\//-per-}"; metric="${metric//\"/}"; metric="${metric//%/pcnt-}"; metric="${metric//\(/}"; metric="${metric//\)/}"; metric="${metric//--/-}";
                #metric=`echo "${h_arr[$iter]}" | awk -F'\' '{print $5}' | sed 's/ /-/g' | sed 's/\//-per-/g' | sed 's/"//g' | sed 's/%/pcnt-/g' | sed 's/(//g' | sed 's/)//g' | sed 's/--/-/g'`;
                if [ "$entity" == "" ] || [ "$metric" == "" ]; then return; fi
                mybarr=${b_arr[$iter]}; mybarr="${mybarr//\"/}";
                #echo "entity is $entity, metric is $metric, h_arr[iter] is ${h_arr[$iter]}, b_arr[iter] is ${mybarr}";
                if [ "$entity" == "$entity_running" ]; then
                	if [ "$mybarr" != "" ]; then
                		if [ "$Str" != "" ]; then
                        		Str="$Str,$metric=${mybarr}"
				else
                        		Str="$metric=${mybarr}"
				fi
			fi
                else 
			if [ "$Str" != "" ]; then echo "esx.Esxtop.$esxtop_category,esxhost=esx_${host_ip},runtag=$runtag,app_name_descr=Host-stats-collector,source=$WFsource,name=${entity_running} $Str" | $DEBUG | socat -t 0 - UDP:localhost:8094; fi
			entity_running=$entity;
                	if [ "$mybarr" != "" ]; then
				Str="$metric=${mybarr}";
			else
				Str="";
			fi
		fi
		if [ $iter -eq $Nelems ]; then echo "esx.Esxtop.$esxtop_category,esxhost=esx_${host_ip},runtag=$runtag,app_name_descr=Host-stats-collector,source=$WFsource,name=${entity_running} $Str" | $DEBUG | socat -t 0 - UDP:localhost:8094; 
		fi
		#if [ $(($iter % 5000)) == 0 ]; then echo "iter is $iter"; date; fi
	}
}
collect_esxtop_all()
{ 
	local host_ip=$1;
	while :
	do
		esxtop_all=`$SSHCMD root@${host_ip} "esxtop -a -b -n 1"`;
		esxtop_process "$esxtop_all" "0" "${host_ip}"
	        sleep $ESXTOP_ALL_SLEEPTIME;
	done
}

collect_esxtop_cpusummary()
{	
	local host_ip=$1;
	while :
	do
		esxtop_cpusummary=`$SSHCMD root@${host_ip} "/tmp/esxtop_cpu.sh | tail -1"`;
		set -- $esxtop_cpusummary;
		echo "esx.Esxtop.CPUSUMMARY,esxhost=esx_${host_ip},runtag=$runtag,app_name_descr=Host-stats-collector,source=$WFsource,name=Total pcpu-used=$3,pcpu-util=$4,core-util=$5" | $DEBUG | socat -t 0 - UDP:localhost:8094 
	        sleep $ESXTOP_CPUSUMMARY_SLEEPTIME;
	done
}

collect_schedstats()
{
	local host_ip=$1;
	while :
	do
		local BODY="";
		local val="";
		local Fields="";
		local line="";
		local WFString="";
	
		BODY=`$SSHCMD root@${host_ip} "sched-stats -t vcpu-state-times" | sed 1d | grep --line-buffered "vmx"`; 
		echo "$BODY" | grep --line-buffered "[0-9]" | while read line; do
			set -- $line;
			Fields=$#;
			case "$Fields" in
				17) echo "esx.Schedstats.vcpu-state-times,runtag=$runtag,esxhost=esx_${host_ip},app_name_descr=Host-stats-collector,source=$WFsource,vcpu=$1,vsmp=${2},name=${3} uptime=${4},charged=${5},sys=${6},sysoverlap=${7},run=${8},wait=${9},waitIdle=${10},ready=${11},costop=${12},maxlimited=${13},vmktime=${14},guestTime=${15},stickyTime=${16},activeSticky=${17}" | $DEBUG | socat -t 0 - UDP:localhost:8094
	        		;;
			esac;
		done
	
		BODY=`$SSHCMD root@${host_ip} "sched-stats -t vcpu-load" | sed 1d | grep --line-buffered "vmx"`; 
		echo "$BODY" | grep --line-buffered "[0-9]" | while read line; do
			set -- $line;
			Fields=$#;
			case "$Fields" in
				11) echo "esx.sched-stats.vcpu-load,runtag=$runtag,esxhost=esx_${host_ip},app_name_descr=Host-stats-collector,source=$WFsource,vcpu=$1,vsmp=${2},name=${3} last-short=${4},medium=${5},long=${6},predict-short=${7},medium=${8},long=${9},runEWMA=${10},sleepEWMA=${11}" | $DEBUG | socat -t 0 - UDP:localhost:8094
				;;
			esac;
		done
		
		BODY=`$SSHCMD root@${host_ip} "sched-stats -t vcpu-comminfo" | sed 1d | grep --line-buffered "vmx"`; 
		echo "$BODY" | grep --line-buffered "[0-9]" | while read line; do
	                set -- $line;
	                Fields=$#;
			if [ $Fields -gt 7 ]; then
	                	echo "esx.sched-stats.vcpu-comminfo,runtag=$runtag,esxhost=esx_${host_ip},app_name_descr=Host-stats-collector,source=$WFsource,vcpu=$1,leader=${2},name=${3},isRel=${4},samePCPU=${8} type=${5},id=${6},rate=${7}" | $DEBUG | socat -t 0 - UDP:localhost:8094
			fi
	                for((num=9;num <= ${Fields};num+=5))
	                {
	                        eval WFString="esx.sched-stats.vcpu-comminfo,runtag=$runtag,esxhost=esx_${host_ip},app_name_descr=Host-stats-collector,source=$WFsource,vcpu=$1,leader=${2},name=${3},isRel=\${$((num))},samePCPU=\${$((num+4))}\ type=\${$((num+1))},id=\${$((num+2))},rate=\${$((num+3))}";
	                	echo "$WFString" | $DEBUG | socat -t 0 - UDP:localhost:8094
	                }
	        done
	
		BODY=`$SSHCMD root@${host_ip} "sched-stats -t vcpu-run-times" | sed 1d | grep --line-buffered "vmx"`; 
		echo "$BODY" | grep --line-buffered "[0-9]" | while read line; do
	                set -- $line;
	                Fields=$#;
	                WFString="esx.sched-stats.vcpu-run-times,runtag=$runtag,esxhost=esx_${host_ip},app_name_descr=Host-stats-collector,source=$WFsource,vcpu=$1,vsmp=${2},name=${3} cpu0=${4}"
	                for((num=5;num <= ${Fields};num++))
	                {
	                        eval val="\${${num}}";
	                        WFString=`echo "$WFString,cpu$((num-4))=${val}"`
	                }
	                echo $WFString | $DEBUG | socat -t 0 - UDP:localhost:8094
	        done
	
		BODY=`$SSHCMD root@${host_ip} "sched-stats -t vcpu-node-run-times" | sed 1d | grep --line-buffered "vmx"`; 
		echo "$BODY" | grep --line-buffered "[0-9]" | while read line; do
	                set -- $line;
	                Fields=$#;
	                WFString="esx.sched-stats.vcpu-node-run-times,runtag=$runtag,esxhost=esx_${host_ip},app_name_descr=Host-stats-collector,source=$WFsource,vcpu=$1,vsmp=${2},name=${3} node0=${4}"
	                for((num=5;num <= ${Fields};num++))
	                {
	                        eval val="\${${num}}";
	                        WFString=`echo "$WFString,node$((num-4))=${val}"`
	                }
	                echo $WFString | $DEBUG | socat -t 0 - UDP:localhost:8094
	        done
	
		BODY=`$SSHCMD root@${host_ip} "sched-stats -t numa-clients" | sed 1d`; 
		echo "$BODY" | grep --line-buffered "[0-9]" | while read line; do
	                set -- $line;
	                Fields=$#;
	                case "$Fields" in
	                        11) echo "esx.sched-stats.numa-clients,runtag=$runtag,esxhost=esx_${host_ip},app_name_descr=Host-stats-collector,source=$WFsource,groupName=$1,groupID=${2},clientID=${3},homeNode=${4},affinity=${5} nWorlds=${6},vmmWorlds=${7},localMem=${8},remoteMem=${9},currLocal%=${10},cummLocal%=${11}" | $DEBUG | socat -t 0 - UDP:localhost:8094
	                        ;;
	                esac;
	        done
	
		BODY=`$SSHCMD root@${host_ip} "sched-stats -t numa-migration" | sed 1d`;
		echo "$BODY" | grep --line-buffered "[0-9]" | while read line; do
	                set -- $line;
	                Fields=$#;
	                case "$Fields" in
	                        9) echo "esx.sched-stats.numa-migration,runtag=$runtag,esxhost=esx_${host_ip},app_name_descr=Host-stats-collector,source=$WFsource,groupName=$1,groupID=${2},clientID=${3} balanceMig=${4},loadMig=${5},localityMig=${6},longTermMig=${7},monitorMig=${8},pageMigRate=${9}" | $DEBUG | socat -t 0 - UDP:localhost:8094
	                        ;;
	                esac;
	        done
	
		BODY=`$SSHCMD root@${host_ip} "sched-stats -t numa-cnode" | sed 1d`;
		echo "$BODY" | grep --line-buffered "[0-9]" | while read line; do
			set -- $line;
			Fields=$#;
			case "$Fields" in
				12) echo "esx.sched-stats.numa-cnode,runtag=$runtag,esxhost=esx_${host_ip},app_name_descr=Host-stats-collector,source=$WFsource,groupName=$1,groupID=${2},clientID=${3},nodeID=${4} time=${5},timePct=${6},memory=${7},memoryPct=${8},anonMem=${9},anonMemPct=${10},avgEpochs=${11},memMigHere=${12}" | $DEBUG | socat -t 0 - UDP:localhost:8094
				;;
			esac;
		done
		
		BODY=`$SSHCMD root@${host_ip} "sched-stats -t numa-pnode" | sed 1d`;
		echo "$BODY" | grep --line-buffered "[0-9]" | while read line; do
			set -- $line;
			Fields=$#;
			case "$Fields" in
				9) echo "esx.sched-stats.numa-pnode,runtag=$runtag,esxhost=esx_${host_ip},app_name_descr=Host-stats-collector,source=$WFsource,nodeID=$1 used=${2},idle=${3},entitled=${4},owed=${5},loadAvgPct=${6},nVcpu=${7},freeMem=${8},totalMem=${9}" | $DEBUG | socat -t 0 - UDP:localhost:8094
				;;
			esac;
		done
	
		BODY=`$SSHCMD root@${host_ip} "sched-stats -t numa-global" | sed 1d`;
		echo "$BODY" | grep --line-buffered "[0-9]" | sed 's/://g' | while read line; do
			set -- $line;
			Fields=$#;
			case "$Fields" in
				2) echo "esx.sched-stats.numa-global,runtag=$runtag,esxhost=esx_${host_ip},app_name_descr=Host-stats-collector,source=$WFsource ${1}=${2}" | $DEBUG | socat -t 0 - UDP:localhost:8094
				;;
			esac;
		done
	
		BODY=`$SSHCMD root@${host_ip} "sched-stats -t ncpus"`;
		echo "$BODY" | grep --line-buffered "[0-9]" | while read line; do
			set -- $line;
			Fields=$#;
			case "$Fields" in
				2) echo "esx.sched-stats.ncpus,runtag=$runtag,esxhost=esx_${host_ip},app_name_descr=Host-stats-collector,source=$WFsource ${2}=${1}" | $DEBUG | socat -t 0 - UDP:localhost:8094
				;;
				3) echo "esx.sched-stats.ncpus,runtag=$runtag,esxhost=esx_${host_ip},app_name_descr=Host-stats-collector,source=$WFsource ${2}${3}=${1}" | $DEBUG | socat -t 0 - UDP:localhost:8094
				;;
			esac;
		done
	
		BODY=`$SSHCMD root@${host_ip} "sched-stats -t cpu" | sed 1d | grep --line-buffered "vmx"`;
		echo "$BODY" | grep --line-buffered "[0-9]" | while read line; do
			set -- $line;
			Fields=$#;
			case "$Fields" in
				22) echo "esx.sched-stats.cpu,runtag=$runtag,esxhost=esx_${host_ip},app_name_descr=Host-stats-collector,source=$WFsource,vcpu=$1,vsmp=${2},type=${3},name=${4},status=${6},wait=${9},units=${15},group=${17},affinity=${22} uptime=${5},usedsec=${7},syssec=${8},waitsec=${10},idlesec=${11},readysec=${12},min=${13},max=${14},shares=${16},emin=${18},cpu=${19},prio=${20},mode=${21}" | $DEBUG | socat -t 0 - UDP:localhost:8094
				;;
			esac;
		done
	        sleep $SCHEDSTATS_SLEEPTIME;
	done
}

collect_memstats()
{
	local host_ip=$1;
	local Fields="";
	local line="";

	while :
	do
		local vmstats=`$SSHCMD root@${host_ip} memstats -r vm-stats` 
		elapsedtime=`echo $(( $SECONDS - $start_time ))`;
		elapsedtime_print=`echo Elapsed time: $elapsedtime`;

		if [ $WRITE_MEMSTATS_TO_DISK -ne 0 ]; then
		 	date >> $folder/esx_${host_ip}.memstats;	
			echo $elapsedtime_print >> $folder/esx_${host_ip}.memstats
			echo "" >> $folder/esx_${host_ip}.memstats;
			echo "$vmstats" >> $folder/esx_${host_ip}.memstats;
		fi
		echo "$vmstats" | grep --line-buffered -v ":" | grep --line-buffered "[0-9]" | while read line; do
			set -- $line;
			Fields=$#;
			case "$Fields" in
	  			31) echo "esx.Mem.Vmstats,esxhost=esx_${host_ip},runtag=$runtag,app_name_descr=Host-stats-collector,source=$WFsource,useReliableMem=$30,name=$1,b=${2},schedGrp=${3},parSchedGroup=${4},worldGrp=${5},memSizeLimit=${6},memSize=${7},min=${8},max=${9},minLimit=${10},shares=${11} ovhdResv=${12},ovhd=${13},allocTgt=${14},consumed=${15},balloonTgt=${16},ballooned=${17},swapTgt=${18},swapped=${19},mapped=${20},touched=${21},zipped=${22},vpmemRO=${23},vpmemRW=${24},vpmemUC=${25},zipSaved=${26},shared=${27},zero=${28},sharedSaved=${29},swapScope=${31}" | $DEBUG | socat -t 0 - UDP:localhost:8094
	  			;;
	  			22) echo "esx.Mem.Vmstats,esxhost=esx_${host_ip},runtag=$runtag,app_name_descr=Host-stats-collector,source=$WFsource,name=$1,memSizeLimit=${2},memSize=${3},min=${4} ovhdResv=${5},ovhd=${6},allocTgt=${7},consumed=${8},balloonTgt=${9},ballooned=${10},swapTgt=${11},swapped=${12},mapped=${13},touched=${14},zipped=${15},vpmemRO=${16},vpmemRW=${17},vpmemUC=${18},zipSaved=${19},shared=${20},zero=${21},sharedSaved=${22}" | $DEBUG | socat -t 0 - UDP:localhost:8094
	  			;;
	  			33) echo "esx.Mem.Vmstats,esxhost=esx_${host_ip},runtag=$runtag,app_name_descr=Host-stats-collector,source=$WFsource,useReliableMem=$32,isUnderPressure=${19},name=$1,b=${2},schedGrp=${3},parSchedGroup=${4},worldGrp=${5},memSizeLimit=${6},memSize=${7},min=${8},max=${9},minLimit=${10},shares=${11} ovhdResv=${12},ovhd=${13},allocTgt=${14},consumed=${15},balloonTgt=${16},ballooned=${17},pressureLimit=${18},swapTgt=${20},swapped=${21},mapped=${22},touched=${23},zipped=${24},vpmemRO=${25},vpmemRW=${26},vpmemUC=${27},zipSaved=${28},shared=${29},zero=${30},sharedSaved=${31},swapScope=${33}" | $DEBUG | socat -t 0 - UDP:localhost:8094
	  			;;
	  			23) echo "esx.Mem.Vmstats,esxhost=esx_${host_ip},runtag=$runtag,app_name_descr=Host-stats-collector,source=$WFsource,name=$1,memSizeLimit=${2},memSize=${3},min=${4} ovhdResv=${5},ovhd=${6},allocTgt=${7},consumed=${8},balloonTgt=${9},ballooned=${10},pressureLimit=${11},swapTgt=${12},swapped=${13},mapped=${14},touched=${15},zipped=${16},vpmemRO=${17},vpmemRW=${18},vpmemUC=${19},zipSaved=${20},shared=${21},zero=${22},sharedSaved=${23}" | $DEBUG | socat -t 0 - UDP:localhost:8094
	  			;;
	  			34) echo "esx.Mem.Vmstats,esxhost=esx_${host_ip},runtag=$runtag,app_name_descr=Host-stats-collector,source=$WFsource,useReliableMem=${33},isUnderPressure=${20},name=$1,b=${2},schedGrp=${3},parSchedGroup=${4},worldGrp=${5},memSizeLimit=${6},memSize=${7},min=${8},max=${9},minLimit=${10},shares=${11} ovhdResv=${12},ovhd=${13},allocTgt=${14},consumed=${15},balloonTgt=${16},ballooned=${17},pressureLimit=${18},memNeeded=${19},swapTgt=${21},swapped=${22},mapped=${23},touched=${24},zipped=${25},vpmemRO=${26},vpmemRW=${27},vpmemUC=${28},zipSaved=${29},shared=${30},zero=${31},sharedSaved=${32},swapScope=${34}" | $DEBUG | socat -t 0 - UDP:localhost:8094
	  			;;
	  			24) echo "esx.Mem.Vmstats,esxhost=esx_${host_ip},runtag=$runtag,app_name_descr=Host-stats-collector,source=$WFsource,name=$1,memSizeLimit=${2},memSize=${3},min=${4} ovhdResv=${5},ovhd=${6},allocTgt=${7},consumed=${8},balloonTgt=${9},ballooned=${10},pressureLimit=${11},memNeeded=${12},swapTgt=${13},swapped=${14},mapped=${15},touched=${16},zipped=${17},vpmemRO=${18},vpmemRW=${19},vpmemUC=${20},zipSaved=${21},shared=${22},zero=${23},sharedSaved=${24}" | $DEBUG | socat -t 0 - UDP:localhost:8094
	  			;;
			esac;
		done


		local gstats=`$SSHCMD root@${host_ip} memstats -r guest-stats` 
		if [ $WRITE_MEMSTATS_TO_DISK -ne 0 ]; then
			echo "$gstats" >> $folder/esx_${host_ip}.memstats;
		fi
		echo "$gstats" | grep --line-buffered -v ":" | grep --line-buffered "[0-9]" | while read line; do
			set -- $line;
			Fields=$#;
			case "$Fields" in
				7) echo "esx.Mem.Gueststats,esxhost=esx_${host_ip},runtag=$runtag,app_name_descr=Host-stats-collector,source=$WFsource,name=$1,schedGrp=${2},parSchedGroup=${3},worldGrp=${4} memTotal=${5},lpageTotal=${6},memNeeded=${7}" | $DEBUG | socat -t 0 - UDP:localhost:8094
				;;
				4) echo "esx.Mem.Gueststats,esxhost=esx_${host_ip},runtag=$runtag,app_name_descr=Host-stats-collector,source=$WFsource,name=$1 memTotal=${2},lpageTotal=${3},memNeeded=${4}" | $DEBUG | socat -t 0 - UDP:localhost:8094
				;;
			esac;
		done

		local lpagestats=`$SSHCMD root@${host_ip} memstats -r lpage-stats`;
		if [ $WRITE_MEMSTATS_TO_DISK -ne 0 ]; then
			echo "$lpagestats" >> $folder/esx_${host_ip}.memstats;
		fi
		echo "$lpagestats" | grep --line-buffered -v ":" | grep --line-buffered "[0-9]" | while read line; do
			set -- $line;
			Fields=$#;
			case "$Fields" in
				13) echo "esx.Mem.Lpagestats,esxhost=esx_${host_ip},runtag=$runtag,app_name_descr=Host-stats-collector,source=$WFsource,name=$1,schedGrp=${2},parSchedGroup=${3},worldGrp=${4} nrBackings=${5},nrAllocSuccess=${6},nrLockSuccess=${7},nrBroken=${8},maxSwapThreshold=${9},nrFailPinned=${10},nrFailSwapped=${11},nrFailPinnedLate=${12},nrFailNoMem=${13}" | $DEBUG | socat -t 0 - UDP:localhost:8094
				;;
				10) echo "esx.Mem.Lpagestats,esxhost=esx_${host_ip},runtag=$runtag,app_name_descr=Host-stats-collector,source=$WFsource,name=$1 nrBackings=${2},nrAllocSuccess=${3},nrLockSuccess=${4},nrBroken=${5},maxSwapThreshold=${6},nrFailPinned=${7},nrFailSwapped=${8},nrFailPinnedLate=${9},nrFailNoMem=${10}" | $DEBUG | socat -t 0 - UDP:localhost:8094
			esac;
		done

		local compstats=`$SSHCMD root@${host_ip} memstats -r comp-stats`; 
		if [ $WRITE_MEMSTATS_TO_DISK -ne 0 ]; then
			echo "$compstats" >> $folder/esx_${host_ip}.memstats;
		fi
		echo "$compstats" | grep --line-buffered -v ":" | grep --line-buffered "[0-9]" | while read line; do
			set -- $line;
			Fields=$#;
			case "$Fields" in
				19) echo "esx.Mem.Compstats,esxhost=esx_${host_ip},runtag=$runtag,app_name_descr=Host-stats-collector,source=$WFsource,memState=${19} total=$1,discarded=${2},managedByMemMap=${3},reliableMem=${4},kernelCode=${5},dataAndHeap=${6},buddyOvhd=${7},rsvdLow=${8},managedByMemSched=${9},minFree=${10},vmkClientConsumed=${11},otherConsumed=${12},free=${13},numHigh=${14},numClear=${15},numSoft=${16},numHard=${17},numLow=${18}" | $DEBUG | socat -t 0 - UDP:localhost:8094
				;;
			esac;
		done


		local groupstats=`$SSHCMD root@${host_ip} memstats -r group-stats`;
		if [ $WRITE_MEMSTATS_TO_DISK -ne 0 ]; then
			echo "$groupstats" >> $folder/esx_${host_ip}.memstats;
		fi
		echo "$groupstats" | grep --line-buffered "[0-9]" | while read line; do
			set -- $line;
			Fields=$#;
			case "$Fields" in
				#33) echo "esx.Mem.Groupstats,esxhost=esx_${host_ip},runtag=$runtag,app_name_descr=Host-stats-collector,source=$WFsource,gid=${1},name=${2} parGid=${3},nChild=${4},min=${5},max=${6},minLimit=${7},shares=${8},conResv=${9},availResv=${10},memSize=${11},memSizePeak=${12},iMin=${13},eMin=${14},eMinPeak=${15},rMinPeak=${16},rMinChildPeak=${17},bMin=${18},bMax${19},bShares${20},fShares=${21},ovhdResv=${22},ovhd=${23},allocTgt=${24},consumed=${25},consumedPeak=${26},touched=${27},bActive=${28},sharedSaved=${29},mps=${30},throttlePct=${31},flags=${32},tag=${33}" | $DEBUG | socat -t 0 - UDP:localhost:8094
				33) echo "esx.Mem.Groupstats,esxhost=esx_${host_ip},runtag=$runtag,app_name_descr=Host-stats-collector,source=$WFsource,gid=${1},name=${2} memSize=${11},consumed=${25},touched=${27}" | $DEBUG | socat -t 0 - UDP:localhost:8094
				;;
			esac;
		done

		local uwstats=`$SSHCMD root@${host_ip} memstats -r uw-stats`;
		if [ $WRITE_MEMSTATS_TO_DISK -ne 0 ]; then
			echo "$uwstats" >> $folder/esx_${host_ip}.memstats;
		fi
		echo "$uwstats" | grep --line-buffered "[0-9]" | while read line; do
			set -- $line;
			Fields=$#;
			case "$Fields" in
				#30) echo "esx.Mem.Uwstats,esxhost=esx_${host_ip},runtag=$runtag,app_name_descr=Host-stats-collector,source=$WFsource type                 name   schedGrp parSchedGroup   worldGrp        min        max   minLimit     shares  totalResv pinnedResv memSizeLimit    memSize  memBStore swapBStore     mapped    touched  swappable        cow   pageable pageableUnacct     pinned prealloced        shm   allocTgt   consumed    swapTgt    swapped useReliableMem swapScope" | $DEBUG | socat -t 0 - UDP:localhost:8094
				30) echo "esx.Mem.Uwstats,esxhost=esx_${host_ip},runtag=$runtag,app_name_descr=Host-stats-collector,source=$WFsource,name=${2} memSize=${13},consumed=${26},touched=${17}" | $DEBUG | socat -t 0 - UDP:localhost:8094
				;;
			esac;
		done


	        sleep $MEMSTATS_SLEEPTIME;
	done

		#TBD - $SSHCMD root@${host_ip_arr[$num]} memstats -r group-stats -g 4 >> $folder/esx_${host_ip_arr[$num]}.memstats; 
}

collect_disk_space()
{
	local host_ip=$1;

	while :
	do
		date >> $folder/esx_${host_ip}.space;	
		echo $elapsedtime_print >> $folder/esx_${host_ip}.space
	       	echo "" >> $folder/esx_${host_ip}.space;	
		local esx_space=`$SSHCMD root@${host_ip} "df -h"`;
		echo "${esx_space}" >> $folder/esx_${host_ip}.space;
	        sleep $ESX_DISKSPACE_SLEEPTIME;
	done
}

# MAIN SCRIPT
if test $# -lt 1; then
 	usage;
   	exit 1
fi

#Default values
HOSTPASS='';
tot_hosts=0;
SSHCMD="sshpass -e ssh -o LogLevel=quiet -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no";
SCPCMD="sshpass -e scp -o LogLevel=quiet -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no";
declare -A WCPID;
declare -A WCPID2;
DEBUG="tee"
folder='/tmp/'
dir=`dirname $0`;
if [ $DEBUG_MODE -eq 1 ]; then
	SSHCMD="sshpass -e ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no";
	SCPCMD="sshpass -e scp -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no";
	DEBUG="tee /dev/tty"
fi

#Catch when the script gets the TERM signal and do the post processing
trap "post_run" TERM SIGINT 
ssh_key=""

while getopts "o:r:p:i:w:k:" ARGOPTS ; do
    case ${ARGOPTS} in
        o) folder="$OPTARG"
            ;;
        r) runtag=$OPTARG
            ;;
        k) ssh_key=$OPTARG
            ;;
        p) HOSTPASS=$OPTARG
            ;;
        i) ip_string=$OPTARG
            ;;
        w) WFsource=$OPTARG
            ;;
        ?) usage
            ;;
    esac
done;
if [ "$runtag" == "" ]; then
	echo "Please provide a runtag for the run using -r";
	exit;
fi
if [ "$ip_string" == "" ]; then
	echo "Please provide a list of hosts to monitor in a comma separated format using -i";
	exit;
fi
if [ "$WFsource" == "" ]; then
	echo "Please provide a source string to identify your data on Wavefront using -w";
	exit;
fi
#Get the Host IP addresses into an array
IFS=',' read -a host_ip_arr <<<"${ip_string}"
tot_hosts=${#host_ip_arr[@]};

if [ "$ssh_key" != "" ]; then
	if ! stat $ssh_key > /dev/null 2>&1; then
        	if stat ~/.ssh/$ssh_key > /dev/null 2>&1; then
                	echo "Looks like the ssh_key file is not provided with an absolute path, picking up $ssh_key from ~/.ssh folder";
			ssh_key="~/.ssh/$ssh_key"
        	else
                	echo "Looks like the config file is not provided with an absolute path. No $ssh_key in ~/.ssh folder either. I will try using the password";
        	fi
	fi
		SSHCMD="$SSHCMD -i $ssh_key"
		SCPCMD="$SCPCMD -i $ssh_key"
fi

#Invoke pre_run on the hosts for preparing and monitoring
pre_run;

#Note the start time to calculate the elapsed time 
start_time=$SECONDS
export SSHPASS=$HOSTPASS;

#Launch subprocesses for each statistic and each host to go in parallel
for((num=0;num < $tot_hosts;num++))
{
	if [ $ESXTOP_ALL -eq 1 ]; then
		collect_esxtop_all ${host_ip_arr[$num]} &
	fi

	if [ $ESXTOP_CPUSUMMARY -eq 1 ]; then
		collect_esxtop_cpusummary ${host_ip_arr[$num]} &
	fi
	
	if [ $SCHEDSTATS -eq 1 ]; then
		collect_schedstats ${host_ip_arr[$num]} &
	fi
	
	if [ $MEMSTATS -eq 1 ]; then
		collect_memstats ${host_ip_arr[$num]} &
	fi
	
	if [ $ESX_DISKSPACE -eq 1 ]; then	
		collect_disk_space ${host_ip_arr[$num]} &
	fi
}
wait;

