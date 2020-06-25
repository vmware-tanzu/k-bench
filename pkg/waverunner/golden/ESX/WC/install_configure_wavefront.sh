#!/bin/bash
# Copyright 2017 VMware, Inc. All rights reserved. -- VMware Confidential

# This function checks if curl is installed. Exits with code 1 if not installed.
check () {
	if ! which $1 > /dev/null; then 
		echo "Please install $1 and try again";
		exit 1;
	fi
}

# This function installs Wavefront proxy on this machine if it is not already installed
install_wavefront_proxy () {
	service_status=`systemctl status wavefront-proxy 2>&1`;
	
	if grep -q "Loaded: loaded" <<< $service_status ; then
	        echo "Wavefront proxy service already installed, moving on...";
	else
		check "curl";
	        sudo -H bash -c "$(curl -sL https://wavefront.com/install)" -- install \
	            --proxy \
	            --wavefront-url https://vmware.wavefront.com \
	            --api-token <Your-Wavefront-Token>
	fi
	service_status=`systemctl status wavefront-proxy 2>&1`;
	if grep -q "Active: active" <<< $service_status ; then
		echo "Wavefront proxy service active moving on...";
	else
		systemctl restart wavefront-proxy;
	fi
}

# This function installs Wavefront agent (telegraf) on this machine if it is not already installed
# Returns 0 if agents is installed, 1 if the agent was already running
install_wavefront_agent () {
	service_status=`systemctl status telegraf 2>&1`;
	
	if grep -q "Active: active" <<< $service_status ; then
	        echo "Wavefront agent already installed, moving on..."; return 1;
	else
		check "curl";
		sudo -H bash -c "$(curl -sL https://wavefront.com/install)" -- install \
		    --agent \
		    --proxy-address localhost --proxy-port 2878 \ 
	fi
	return 0;
}

# Start the wavefront agent with the settings for this run, including the host name string to enable lookup on Wavefront under source and runtag that can be a unique identifier for this run
start_wavefront_agent ()
{
	[ $# -eq 0 ] && { echo "Usage: $0 -p <proxy_address> -f <1_to_force_restart> -h <host_name> -r <run_tag> -v <vm_name>"; exit; }

	local OPTIND	
	while getopts "h:v:r:f:p:" MYOPTS ; do
	    case ${MYOPTS} in
	        h) host_name="$OPTARG"
	            ;;
	        r) runtag=$OPTARG
	            ;;
	        f) force_restart=$OPTARG
	            ;;
	        p) proxy=$OPTARG
	            ;;
	        v) vmname=$OPTARG
	            ;;
	        ?) usage
	            ;;
	    esac
	done;
	
	# Debug - echo "proxy is $proxy, force restart is $force_restart, runtag is $runtag, host_name is $host_name";
	# Check if the proxy, host_name, runtag are the same already in the running version of the agent. Given that case and if force_restart is also zero, nothing to do here
	if grep -q "host = \"$proxy\"" /etc/telegraf/telegraf.conf && grep -q "$host_name" /etc/telegraf/telegraf.conf && grep -q "$vmname" /etc/telegraf/telegraf.conf && grep -q "$runtag" /etc/telegraf/telegraf.conf && [ "$force_restart" == "0" ]; then
		echo "Not reconfiguring the agent as all the settings are the same";
		return 1;
	else
		echo "proxy is $proxy, host_name is $host_name, runtag is $runtag, force_restart is $force_restart, vmname is $vmname";
		force_restart=1;
	fi
	#Replace localhost with the given proxy information in config file
	cp $dir/telegraf.conf /tmp/telegraf_temp.conf;
	sed -i "s/host = \"localhost\"/host = \"$proxy\"/g" /tmp/telegraf_temp.conf
	if [ "$host_name" != "" ]; then
		sed -i "s/#hostname = \"perf-host\"/hostname = \"${host_name}\"/g" /tmp/telegraf_temp.conf;
	fi
	if [ "$runtag" != "" ]; then
		line=`sed -n '/$USER/=' /tmp/telegraf_temp.conf`
		sed "${line}iruntag=\"$runtag\"" /tmp/telegraf_temp.conf > /tmp/telegraf_temp2.conf
		mv /tmp/telegraf_temp2.conf /tmp/telegraf_temp.conf; 
	fi
	if [ "$vmname" != "" ]; then
		line=`sed -n '/$USER/=' /tmp/telegraf_temp.conf`
		sed "${line}iVMname = \"$vmname\"" /tmp/telegraf_temp.conf > /tmp/telegraf_temp2.conf
		mv /tmp/telegraf_temp2.conf /tmp/telegraf_temp.conf; 
	fi
	#Restart the telegraf service with the new proxy
	if [ "$force_restart" == "1" ]; then
		sudo cp /tmp/telegraf_temp.conf /etc/telegraf/telegraf.conf
		rm -rf /etc/telegraf/telegraf.d/* 2> /dev/null;
		echo "Restarting Wavefront agent..."
		systemctl restart telegraf 
	fi
	rm /tmp/telegraf_temp.conf;
	
	#Just for reference, if we want to manually start the telegraf agent
	#telegraf --config /etc/telegraf/telegraf.conf --input-filter socket_listener --output-filter wavefront &
}

# This installs the python client pytelegraf that we can use in python scripts to send data to Wavefront
install_wavefront_client ()
{
	# First check if pip is installed, if not install pip
	if ! which pip > /dev/null; then
		check_curl;
		curl "https://bootstrap.pypa.io/get-pip.py" -o "get-pip.py";
		python get-pip.py;
	else
		echo "pip already installed, moving on...";
	fi
	
	# Check if pytelegraf is installed, if not install pytelegraf
	install_status=`pip freeze | grep pytelegraf`;
	
	if grep -q "pytelegraf" <<< $install_status ; then
	        echo "Wavefront client already installed, moving on...";
	else
	        pip install pytelegraf;
	fi
}

usage ()
{
	echo "Usage: $0 [-p <proxy_address> -f <1_to_force_restart> -h <host_name> -r <run_tag> -v <vm_name>]"; 
	echo "If no WF proxy info provided and no local proxy already running, one gets installed on this machine." 
	echo "The provided host_name is the value to search for, under sources on Wavefront. Defaults to os.Hostname()"
	exit; 
}

# Record the directory where this script resides, will be expecting telegraf.conf in this folder too
dir=`dirname $0`;
force_restart=0;
proxy="";
run_tag="";
host_name=""

while getopts "h:r:p:f:v:" ARGOPTS ; do
    case ${ARGOPTS} in
        h) host_name="$OPTARG"
            ;;
        r) run_tag=$OPTARG
            ;;
        f) force_restart=$OPTARG
            ;;
        p) proxy=$OPTARG
            ;;
        v) vm_name=$OPTARG
            ;;
        ?) usage
            ;;
    esac
done;

if [ "${proxy}" == "" ]; then
	install_wavefront_proxy;
	proxy="localhost"
fi

install_wavefront_agent;
agentinstalled=$?;

if [ "$agentinstalled" == "0" ] || [ "${force_restart}" == "1" ]; then
	start_wavefront_agent -p "$proxy" -f "1" -h "$host_name" -r "$run_tag" -v "$vm_name";
else
	start_wavefront_agent -p "$proxy" -f "0" -h "$host_name" -r "$run_tag" -v "$vm_name";
fi

exit 0;
#install_wavefront_client;
