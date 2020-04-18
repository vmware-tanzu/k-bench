#!/bin/bash
dir=`dirname $0`;

check () {
        if ! which $1 > /dev/null; then
                echo "Please install $1 and try again";
                exit 1;
	else
		echo "$1 is installed"
        fi
}

echo "Ensure env variable HOME is set to your home directory, proceeding under that assumption...";

check curl;
check socat;
check sshpass;

sudo $dir/WF/install_configure_wavefront.sh;
