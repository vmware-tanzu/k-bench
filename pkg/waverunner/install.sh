#!/bin/bash
dir=`dirname $0`;

usage () {
    echo "Usage: $0 -k <Wavefront_API_token> -u <Wavefront_URL>"
    echo "Wavefront URL defaults to https://vmware.wavefront.com"
    echo "To get your Wavefront API token, please refer to https://docs.wavefront.com/wavefront_api.html#generating-an-api-token"
    echo "";
}


if [ $# -eq 0 ]
  then
  usage
  exit;
fi

url="https://vmware.wavefront.com"

while getopts "k:u:" ARGOPTS ; do
    case ${ARGOPTS} in
        k) token=$OPTARG
            ;;
        u) url=$OPTARG
            ;;
        ?) usage; exit;
            ;;
    esac
done


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
check bc;

#sudo $dir/golden/WF/install_configure_wavefront_linux.sh $url $token;
echo "sudo $dir/golden/WF/install_configure_wavefront_linux.sh -u $url -k $token";
