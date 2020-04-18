#!/bin/bash
dir=`dirname $0`;

check () {
        if ! which $1 > /dev/null; then
                echo "$1 is not installed, Let me try installing all dependencies using puppet";
        	if ! which puppet > /dev/null; then
                	echo "Please install puppet and try again";
                	exit 1;
		else
			sudo puppet apply $dir/waverunner_driver.pp -v
			if ! which $1 > /dev/null; then
				echo "Please install $1 manually and try again";
                		exit 1;
			fi
		fi
	else
		echo "$1 is installed"
        fi
}

echo "Ensure env variable HOME is set to your home directory, proceeding under that assumption...";

check curl;
check socat;
check rsync;
check sshpass;

sudo $dir/golden/WF/install_configure_wavefront_linux.sh;
