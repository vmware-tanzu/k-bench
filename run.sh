#!/bin/bash
dir=`dirname $0`;

usage () {
    echo "Usage: $0 -r <run-tag> [-t <comma-separated-tests> -c <config-dir> -o <output-dir>]"
    echo "Example: $0 -r \"kbench-run-on-XYZ-cluster\"  -t \"cp_heavy16,dp_netperf_internode,dp_fio\" -o \"./\""
    echo "";
    echo "Valid test names:"
    echo "";
    echo "all|all_control_plane|all_data_plane|$tests" | sed 's/|/ || /g'
}

configdir="${dir}/config/";
tag="run";
outdir="$dir";


while getopts "r:t:c:o:h" ARGOPTS ; do
    case ${ARGOPTS} in
        t) tests=$OPTARG
            ;;
        c) configdir=$OPTARG
            ;;
        r) tag=$OPTARG
            ;;
        o) outdir=$OPTARG
            ;;
        h) usage; exit;
            ;;
        ?) usage; exit;
            ;;
    esac
done


if [ $# -eq 0 ]
  then
  usage
  echo "Since no tests specified, I am running the default workload: config/default";
  tests="default"
fi

if [ -z "${tests}" ]; then
  tests=`ls -b $configdir | tr '\n' ','`
fi

folder=`date '+%d-%b-%Y-%I-%M-%S-%P'`
folder="$outdir/results_${tag}_$folder"
mkdir $folder

if grep -q "all_data_plane" <<< $tests; then
	dptests=`ls -b $configdir | grep "dp_" | tr '\n' ','`
	tests=`echo $tests | sed "s/all_data_plane/$dptests/g"`
fi

if grep -q "all_control_plane" <<< $tests; then
	cptests=`ls -b $configdir | grep "cp_" | tr '\n' ','`
	cptests="default,$cptests"
	tests=`echo $tests | sed "s/all_control_plane/$cptests/g"`
fi

if grep -q "all" <<< $tests; then
	alltests=`ls -b $configdir | tr '\n' ','`
	tests=`echo $tests | sed "s/all/$alltests/g"`
fi

tests=`echo $tests | sed "s/,/ /g"`

for test in $tests; do
	mkdir $folder/$test;
	cp $configdir/$test/config.json $folder/$test/;
	cp $configdir/$test/*.yaml $folder/$test/ > /dev/null 2>&1;
	cp $configdir/$test/*.sh $folder/$test/ > /dev/null 2>&1;
	echo "Running test $test found in $configdir and results redirected to \"$folder/$test\"";
	if [ "$test" == "dp_fio" ]; then
		kubectl apply -f ${configdir}/dp_fio/fio_pvc.yaml
	fi
	kbench -benchconfig="$configdir/$test/config.json" -outdir="$folder/$test";
	$dir/cleanup.sh > /dev/null 2>&1;
done
