#!/bin/bash

usage () {
        echo "Usage: $0 -r <run_tag> -i <Host_IP_String> -w <Wavefront_source> [-o <output_folder> -k <ssh_key_file> -p <host_passwd>]";
        echo "Defaults to /tmp for output folder and a null host password" 
        exit;
}

post_run() 
{
	if [ "${HOST_OS}" == "ESXi" ]; then
		kill -TERM ${WCPID[${host_ip_arr[0]}]};
                echo "Waiting on ESX monitor process to terminate";
                wait ${WCPID[${host_ip_arr[0]}]};
	else
                echo "Terminating monitoring processes";
		for((num=0;num < $tot_hosts;num++))
		{
			$SSHCMD ${USER}@${host_ip_arr[$num]} "ps -elf | grep collect_guest | grep -v grep | awk {'print \$4'} | xargs kill -TERM & > /dev/null"
			kill -TERM ${WCPID[${host_ip_arr[$num]}]}; 
		}
	fi
}

# MAIN SCRIPT
if test $# -lt 1; then
 	usage;
   	exit 1
fi

#Default values
DEBUG_MODE=0;
USER="root"
HOSTPASS='';
tot_hosts=0;
SSHCMD="sshpass -e ssh -o LogLevel=quiet -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no";
SCPCMD="sshpass -e scp -o LogLevel=quiet -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no";
declare -A WCPID;
DEBUG="tee"
folder='/tmp/'
runtag="kbench-run";
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
if [ "$ip_string" == "" ]; then
	echo "Please provide a list of hosts to monitor in a comma separated format using -i";
	usage;
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

export SSHPASS=$HOSTPASS;
HOST_OS=`$SSHCMD root@${host_ip_arr[0]} "uname -o"`;
if [ "${HOST_OS}" == "ESXi" ]; then
	echo "Node OS is ESXi, preparing to monitor";
	if [ "${ssh_key}" != "" ]; then
		$dir/scripts/config_monitor_hosts.sh  -r $runtag -i ${ip_string} -w $WFsource -o $folder -k ${ssh_key} -p "$HOSTPASS" &
	else
		$dir/scripts/config_monitor_hosts.sh  -r $runtag -i ${ip_string} -w $WFsource -o $folder -p "$HOSTPASS" &	
	fi	
	WCPID[${host_ip_arr[0]}]=$!;	
else
	#Launch subprocesses too install the needed bits on each Linux node to go in parallel
	echo "Node OS is ${HOST_OS}, I am starting to monitor";
	for((num=0;num < $tot_hosts;num++))
	{
		#configure WF wiring
	        $SCPCMD $dir/golden/WF/install_configure_wavefront_linux.sh ${USER}@${host_ip_arr[$num]}:/tmp/
	        $SCPCMD $dir/golden/WF/telegraf_linux.conf ${USER}@${host_ip_arr[$num]}:/tmp/
	        $SCPCMD $dir/golden/WF/waverunner_guest.pp ${USER}@${host_ip_arr[$num]}:/tmp/
	        $SCPCMD $dir/golden/LIN/collect_guest_stats.sh ${USER}@${host_ip_arr[$num]}:/tmp/
	        $SSHCMD ${USER}@${host_ip_arr[$num]} "/tmp/install_configure_wavefront_linux.sh" &
	}

	#Wait until all installations are done
	wait;

	echo "Starting to monitor";
        #Start stats collection
	for((num=0;num < $tot_hosts;num++))
	{
		$SSHCMD ${USER}@${host_ip_arr[$num]} "ps -elf | grep collect_guest | grep -v grep | awk {'print \$4'} | xargs kill -TERM > /dev/null &"
               	$SSHCMD ${USER}@${host_ip_arr[$num]} "/tmp/collect_guest_stats.sh -t 30 -o '/tmp/stats' -w $WFsource -d "Kubenode-stats-collector" -i Node$num  -r $runtag" &
		
		WCPID[${host_ip_arr[$num]}]=$!;	
		echo "launched, PID is ${WCPID[${host_ip_arr[$num]}]}"
	}
fi
wait;
