#!/bin/bash
# Copyright 2017 VMware, Inc. All rights reserved. -- VMware Confidential

# This function checks if a package is installed. Exits with code 1 if not installed.
check () {
	if ! which $1 > /dev/null; then 
		echo "Please install $1 and try again";
		exit 1;
	fi
}

# This function installs Wavefront proxy on this machine if it is not already installed
install_wavefront_proxy () {

	if stat "/cygdrive/c/Program Files (x86)/Wavefront" > /dev/null 2>&1; then
	        echo "Wavefront proxy service already installed, moving on...";
		service_status=`cd "/cygdrive/c/Program Files (x86)/Wavefront/bin/"; ./nssm.exe start WavefrontProxy 2>&1 | tr -d '\0'`;
		service_status=`cd "/cygdrive/c/Program Files (x86)/Wavefront/bin/"; ./nssm.exe status WavefrontProxy 2>&1 | tr -d '\0'`;
		if grep -q "RUNNING" <<< $service_status ; then
			return 1;
		else
			echo "Could not start the proxy, reinstalling";
			cd;
		fi
	fi
	echo "Installing Wavefront Proxy..."
	wget https://s3-us-west-2.amazonaws.com/wavefront-cdn/windows/wavefront-proxy-setup.exe
	chmod 755 ./wavefront-proxy-setup.exe
	./wavefront-proxy-setup.exe /server=https://vmware.wavefront.com/api/ /token=<Your-Wavefront-Token> /SILENT
	service_status=`cd "/cygdrive/c/Program Files (x86)/Wavefront/bin/"; ./nssm.exe status WavefrontProxy 2>&1 | tr -d '\0'`;
	if ! grep -q "RUNNING" <<< $service_status ; then
	        echo "Problem starting Wavefront Proxy";
		exit 1;
	fi
	return 0;
}

# This function installs Wavefront agent (telegraf) on this machine if it is not already installed
# Returns 0 if agents is installed, 1 if the agent was already running
install_wavefront_agent () {
	if stat "/cygdrive/c/Program Files/Telegraf" > /dev/null 2>&1; then
	        echo "Wavefront agent seems to be already installed, moving on..."
		service_status=`cmd /c net start telegraf 2>&1`;
		service_status=`cmd /c net start telegraf 2>&1`;
		if grep -q "requested service has already been started" <<< $service_status ; then
			return 1;
		else
			echo "Unable to start the agent, service_status is $service_status \n Reinstalling the agent";
			rm -rf "/cygdrive/c/Program Files/Telegraf/"/* 2> /dev/null; 
		fi
	fi
        echo "Installing Wavefront agent..."
	wget https://dl.influxdata.com/telegraf/releases/telegraf-1.7.0_windows_amd64.zip
	unzip telegraf-1.7.0_windows_amd64.zip
	mkdir "C:\Program Files\Telegraf" > /dev/null 2>&1
	mv telegraf/* "C:\Program Files\Telegraf/"
	/cygdrive/c/Program\ Files/Telegraf/telegraf.exe --service install
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
	if grep -q "host = \"$proxy\"" "/cygdrive/c/Program Files/Telegraf/telegraf.conf" && grep -q "$host_name" "/cygdrive/c/Program Files/Telegraf/telegraf.conf" && grep -q "$vmname" "/cygdrive/c/Program Files/Telegraf/telegraf.conf" && grep -q "$runtag" "/cygdrive/c/Program Files/Telegraf/telegraf.conf" && [ "$force_restart" == "0" ]; then
		echo "Not reconfiguring the agent as all the settings already seem to the same";
		service_status=`cmd /c net start telegraf 2>&1`;
		if grep -q "requested service has already been started" <<< $service_status ; then
			return 1;
		else
			echo "For some reason, the installed service is not active. Reconfiguring and restarting"
			force_restart=1;
		fi
	else
		echo "Reconfiguring and restarting, proxy is $proxy, host_name is $host_name, runtag is $runtag, force_restart is $force_restart, vmname is $vmname";
		force_restart=1;
	fi
	#Replace localhost with the given proxy information in config file
	cp $dir/telegraf_windows.conf $dir/telegraf_temp.conf;
	sed -i "s/host = \"localhost\"/host = \"$proxy\"/g" $dir/telegraf_temp.conf
	if [ "$host_name" != "" ]; then
		sed -i "s/#hostname = \"perf-host\"/hostname = \"${host_name}\"/g" $dir/telegraf_temp.conf;
	fi
	if [ "$runtag" != "" ]; then
		line=`sed -n '/$USER/=' $dir/telegraf_temp.conf`
		sed "${line}iruntag=\"$runtag\"" $dir/telegraf_temp.conf > $dir/telegraf_temp2.conf
		mv $dir/telegraf_temp2.conf $dir/telegraf_temp.conf; 
	fi
	if [ "$vmname" != "" ]; then
		line=`sed -n '/$USER/=' $dir/telegraf_temp.conf`
		sed "${line}iVMname = \"$vmname\"" $dir/telegraf_temp.conf > $dir/telegraf_temp2.conf
		mv $dir/telegraf_temp2.conf $dir/telegraf_temp.conf; 
	fi
	#Restart the telegraf service with the new proxy
	if [ "$force_restart" == "1" ]; then
		cp $dir/telegraf_temp.conf "/cygdrive/c/Program Files/Telegraf/telegraf.conf"
		rm -rf "/cygdrive/c/Program Files/Telegraf/telegraf.d/"/* 2> /dev/null;
		echo "Restarting Wavefront agent..."
		cmd /c net stop telegraf
		cmd /c net start telegraf
	fi
	rm $dir/telegraf_temp.conf;
	
	#Just for reference, if we want to manually start the telegraf agent
	#telegraf --config /etc/telegraf/telegraf.conf --input-filter socket_listener --output-filter wavefront &
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

while getopts "h:v:r:p:f:" ARGOPTS ; do
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
