#!/bin/bash

GOVERSION="1.13.7"
MY_GOPATH="$HOME/go"
MY_GOROOT="$HOME/go-root/go"

DIR="$( cd "$(dirname "$0")" ; pwd -P )";

set_env () {
    echo "Checking env..."
    if [[ -z "${GOPATH}" ]]; then
        echo "Environment variable GOPATH not found, use $MY_GOPATH"
    else
        MY_GOPATH=$GOPATH
    fi
    if [[ -z "${GOROOT}" ]]; then
        echo "Environment variable GOROOT not found, use $MY_GOROOT"
    else
        MY_GOROOT=$GOROOT
    fi
}

install () {
    mkdir -p $MY_GOPATH/src/
    cp -r $DIR $MY_GOPATH/src/
    cd $MY_GOPATH/src/k-bench
    #echo "Getting benchmark's Go dependencies..."
    #go get -d ./...
    if [ -d "$MY_GOPATH/pkg/mod/github.com/kubernetes/kompose"* ]; then
       echo "Kompose already installed."
    else
       echo "Installing kompose..."
       if [ -f "$MY_GOROOT/bin/go" ]; then
           $MY_GOROOT/bin/go get -u github.com/kubernetes/kompose
       else
           go get -u github.com/kubernetes/kompose
       fi
    fi
    echo "Installing k-bench..."
    if [ -f "$MY_GOROOT/bin/go" ]; then
        $MY_GOROOT/bin/go install cmd/kbench.go
    else
        go install cmd/kbench.go
    fi

    #$MY_GOROOT/bin/go install cmd/kbench.go
    #go build ./kbench
    cp $MY_GOPATH/bin/kbench /usr/local/bin/
    if [ $? -ne 0 ]; then
        echo "Tried to copy kbench to /usr/local/bin but failed. If you are not root, run from $MY_GOPATH/bin/kbench"
    fi
    echo "Completed k-bench installation. To rebuild the benchmark, just run \"go install cmd/kbench.go\""
}

install_go () {
    GOFILE="go$GOVERSION.linux-amd64.tar.gz"
    echo ""
    if [ -d $MY_GOROOT ]; then
        echo "Installation directories already exist $MY_GOROOT"
	read -p "Would you like to overwrite (Y/n)? " -n 1 -r
	echo
	if [[ ! $REPLY =~ ^[Yy]$ ]]; then
	    exit 1
        fi
    fi

    mkdir -p "$MY_GOROOT" "$MY_GOPATH" "$MY_GOPATH/src" "$MY_GOPATH/pkg" "$MY_GOPATH/bin"

    wget https://dl.google.com/go/$GOFILE -O $HOME/$GOFILE
    if [ $? -ne 0 ]; then
        echo "Go download failed! Exiting."
        exit 1
    fi

    tar -C $(dirname $MY_GOROOT) -xzf $HOME/$GOFILE

    read -p "Would you like to add Go to your profile .bashrc (Y/n)? " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
	if grep -q "MY_GOROOT" "$HOME/.bashrc"; then
            echo "GOROOT already found in .bashrc."
	else
	    cp -f "$HOME/.bashrc" "$HOME/.bashrc.bkp"

            touch "$HOME/.bashrc"
            {
                echo ''
                echo '# GOLANG'
                echo 'export GOROOT='$MY_GOROOT
                echo 'export GOPATH='$MY_GOPATH
                echo 'export GOBIN=$GOPATH/bin'
                echo 'export PATH=$PATH:$GOROOT/bin:$GOBIN'
                echo ''
            } >> "$HOME/.bashrc"
        fi
    fi
    source $HOME/.bashrc

    #cp $MY_GOROOT/bin/go /usr/local/bin/
}

check () {
    if ! which $1 > /dev/null; then
        echo "$1 is not installed, let me try to install";
        install_go
    else
	echo "$1 is installed"
    fi
}

set_env;

echo "Start to install the benchmark and tools...";

check go;

install;

#echo "Start to install waverunner for host stats collection..."

#sudo $dir/waverunner/install.sh;

#if [ $? -ne 0 ]; then
#    echo "Waverunner installation failed, you can still run the benchmark. Exiting."
#    exit 1
#fi
