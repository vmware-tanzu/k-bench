#!/bin/bash
function usage {
    echo -e "Usage:\t$0 <-n SAMPLES> <-t SAMPLE_TIME> <-o results_root> <-w wavefront_source> <-i identity> <-r run_tag> <-d mydescriptor> [-p]"
}
trap "trap - SIGTERM && kill -- -$$" SIGINT SIGTERM EXIT

SAMPLE_TIME=30
SAMPLES=1000000000
PERF=0
mkdir /stats > /dev/null 2>&1
RESULTS_ROOT="/stats"
while getopts o:n:t:r:i:w:d:p OPT ; do
    case ${OPT} in
	o) RESULTS_ROOT=$OPTARG
	    ;;
	p) PERF=1
	    ;;
	t) SAMPLE_TIME=$OPTARG
	    ;;
	n) SAMPLES=$OPTARG
	    ;;
	i) NAME=$OPTARG
	    ;;
	w) SOURCE=$OPTARG
	    ;;
	r) RUNTAG=$OPTARG
	    ;;
	d) DESC=$OPTARG
	    ;;
	?) usage
	   exit 1
	    ;;
    esac
done
#if test $SAMPLES -eq 0 -o $SAMPLE_TIME -eq 0 -o $RESULTS_ROOT = "null"; then
#    usage
#    exit 1
#fi

mkdir -p $RESULTS_ROOT
if test -f ${RESULTS_ROOT}/RUNID; then
    RUNID=`cat ${RESULTS_ROOT}/RUNID`
else
    RUNID=0
fi
RUNID=`expr $RUNID + 1`
echo $RUNID > ${RESULTS_ROOT}/RUNID
RUN_DIR=${RESULTS_ROOT}/${RUNID}
mkdir -p ${RUN_DIR}

date > ${RUN_DIR}/config.out 2>&1
cat /proc/version >> ${RUN_DIR}/config.out 2>&1
uptime >> ${RUN_DIR}/config.out 2>&1
numactl -H >> ${RUN_DIR}/config.out 2>&1
echo ipcs >> ${RUN_DIR}/config.out 2>&1
ipcs >> ${RUN_DIR}/config.out 2>&1
echo /proc/meminfo >> ${RUN_DIR}/config.out 2>&1
cat /proc/meminfo >> ${RUN_DIR}/config.out 2>&1
echo /proc/swaps >> ${RUN_DIR}/config.out 2>&1
cat /proc/swaps >> ${RUN_DIR}/config.out 2>&1
echo /proc/cpuinfo >> ${RUN_DIR}/config.out 2>&1
cat /proc/cpuinfo >> ${RUN_DIR}/config.out 2>&1
cat /etc/fstab >> ${RUN_DIR}/config.out 2>&1
df >> ${RUN_DIR}/config.out 2>&1
/sbin/sysctl -a >> ${RUN_DIR}/config.out 2>&1
echo /proc/partitions >> ${RUN_DIR}/config.out 2>&1
cat /proc/partitions >> ${RUN_DIR}/config.out 2>&1
cat /proc/interrupts >> ${RUN_DIR}/config.out 2>&1
for DEVNAME in `netstat -r | grep -vE 'routing table|Destination' | awk '{print $NF}' | sort|uniq`; do ethtool $DEVNAME; ethtool -S $DEVNAME; done >> ${RUN_DIR}/config.out 2>&1
cp -rp /proc/sys ${RUN_DIR} 2>/dev/null
for f in /sys/class/fc_host/*; do
    ls $f/device/../irq
    if test $? -ne 0; then
        continue
    fi
    cat $f/device/../irq
done >> ${RUN_DIR}/config.out 2>&1
for IRQ in `grep pvscsi /proc/interrupts | awk '{print $1}' | sed -e "s/:.*//"`; do
    echo $IRQ
    cat /proc/irq/$IRQ/smp_affinity
done >> ${RUN_DIR}/config.out 2>&1
/sbin/vgdisplay -v 2>/dev/null | grep -E ' VG Name|/dev/sd' | uniq | awk '/VG Name/{print}/PV Name/{print; split($NF, a, "/"); gsub("[0-9][0-9]*", "", a[3]); system("ls -d /sys/block/" a[3] "/device/scsi_device*")}' >> ${RUN_DIR}/config.out 2>&1

find /sys/module/pvscsi -type f -print -exec cat {} \; >> ${RUN_DIR}/config.out 2>&1
find /sys/module/vmxnet* -type f -print -exec cat {} \; >> ${RUN_DIR}/config.out 2>&1
vmware-config-tools.pl -h 2>&1 | head -1 >> ${RUN_DIR}/config.out 2>&1
#

IOSTAT_SAMPLES=`expr $SAMPLES + 1 `
STDY_STATE_SEC=`expr $SAMPLES \* $SAMPLE_TIME `

    date > ${RUN_DIR}/interrupts
    cat /proc/interrupts >> ${RUN_DIR}/interrupts
    date > ${RUN_DIR}/netstat.out
    netstat -s >> ${RUN_DIR}/netstat.out

    iostat ${SAMPLE_TIME} ${IOSTAT_SAMPLES} -kxt | grep --line-buffered -v "Linux" | grep --line-buffered "[0-9]\.[0-9][0-9]" | while read line; do 
	set -- $line;
	Fields=$#;
	case "$Fields" in

		14) echo "lin.Gueststats.IO,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE,device=$1 RdReqMgPerSec=$2,WrReqMgPerSec=$3,RdsPerSec=$4,WrPerSec=$5,RkBPerSec=$6,WrkBPerSec=$7,AvgReqSz=$8,AvgQlen=$9,AvgWtTime=${10},AvgWtTmRd-ms=${11},AvgWtTmWr-ms=${12},AvgSvcTm-ms=${13},PcntUtil=${14}" | socat -t 0 - UDP:localhost:8094
		;;
	esac
    done &

    iostat ${SAMPLE_TIME} ${IOSTAT_SAMPLES} -kd | grep --line-buffered -v "Linux" | grep --line-buffered "[0-9]\.[0-9][0-9]" | while read line; do 
	set -- $line;
	Fields=$#;
	case "$Fields" in

		6) echo "lin.Gueststats.IO,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE,device=$1 TrnPerSec=$2,RdKBPerSec=$3,WrKBPerSec=$4,KBRead=$5,KBWrtn=$6" | socat -t 0 - UDP:localhost:8094
		;;
	esac
    done &
    # Redundant stats copy on the local FS, if not needed, comment line below 
    iostat -kxtz ${SAMPLE_TIME} ${IOSTAT_SAMPLES} > ${RUN_DIR}/iostat.out &

    vmstat ${SAMPLE_TIME} ${IOSTAT_SAMPLES} | grep --line-buffered "[0-9]" | while read line; do
        set -- $line;
        Fields=$#;
        case "$Fields" in

                17) echo "lin.Gueststats,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE System.Processes.run=$1,System.Processes.unintsleep=$2,Memory.SwapdKB=$3,Memory.swapdinKBPerSec=$7,Memory.swapdKBoutPerSec=$8,System.Processes.CswitchPerSec=${12}" | socat -t 0 - UDP:localhost:8094
                ;;
        esac
    done &
    # Redundant stats copy on the local FS, if not needed, comment line below 
    vmstat ${SAMPLE_TIME} ${IOSTAT_SAMPLES} > ${RUN_DIR}/vmstat.out &
    
    mpstat -I ALL -P ALL ${SAMPLE_TIME} ${SAMPLES} | grep --line-buffered -v "Linux" | grep --line-buffered "[0-9]\.[0-9][0-9]" | while read line; do 
	set -- $line;
	Fields=$#;
	case "$Fields" in

		13) echo "lin.Gueststats.Interrupts,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE,cpu=$3 HIPerSec=$4,TIMERPerSec=$5,NET-TXPerSec=$6,NET-RXPerSec=$7,BLOCKPerSec=$8,BLOCK-IOPOLLPerSec=$9,TASKLETPerSec=${10},SCHEDPerSec=${11},HRTIMERPerSec=${12},RCUPerSec=${13}" | socat -t 0 - UDP:localhost:8094
		;;
		4) echo "lin.Gueststats.Interrupts,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE,cpu=$3 TotIntrPerSec=$4" | socat -t 0 - UDP:localhost:8094
		;;
		65) echo "lin.Gueststats.Interrupts_CPUBrkDown,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE,cpu=$3 Intr0PerSec=$4,Intr1PerSec=$5,Intr6PerSec=$6,Intr8PerSec=$7,Intr9PerSec=$8,Intr12PerSec=$9,Intr14PerSec=${10},Intr15PerSec=${11},Intr16PerSec=${12},Intr24PerSec=${13},Intr25PerSec=${14},Intr26PerSec=${15},Intr27PerSec=${16},Intr28PerSec=${17},Intr29PerSec=${18},Intr30PerSec=${19},Intr31PerSec=${20},Intr32PerSec=${21},Intr33PerSec=${22},Intr34PerSec=${23},Intr35PerSec=${24},Intr36PerSec=${25},Intr37PerSec=${26},Intr38PerSec=${27},Intr39PerSec=${28},Intr40PerSec=${29},Intr41PerSec=${30},Intr42PerSec=${31},Intr43PerSec=${32},Intr44PerSec=${33},Intr45PerSec=${34},Intr46PerSec=${35},Intr47PerSec=${36},Intr48PerSec=${37},Intr49PerSec=${38},Intr50PerSec=${39},Intr51PerSec=${40},Intr52PerSec=${41},Intr53PerSec=${42},Intr54PerSec=${43},Intr55PerSec=${44},Intr56PerSec=${45},Intr57PerSec=${46},Intr58PerSec=${47},Intr59PerSec=${48},Intr60PerSec=${49},Intr61PerSec=${50},NMIPerSec=${51},LOCPerSec=${52},SPUPerSec=${53},PMIPerSec=${54},IWIPerSec=${55},RTRPerSec=${56},RESPerSec=${57},CALPerSec=${58},TLBPerSec=${59},TRMPerSec=${60},THRPerSec=${61},MCEPerSec=${62},MCPPerSec=${63},ERRPerSec=${64},MISPerSec=${65}" | socat -t 0 - UDP:localhost:8094
		;;
	esac
    done &

    mpstat -u -P ALL ${SAMPLE_TIME} ${SAMPLES} | grep --line-buffered -v "Linux" | grep --line-buffered "[0-9]\.[0-9][0-9]" | while read line; do 
	set -- $line;
	Fields=$#;
	case "$Fields" in

		13) echo "lin.Gueststats.CPU,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE,cpu=$3 user=$4,nice=$5,system=$6,iowait=$7,irq=$8,soft=$9,steal=${10},guest=${11},gnice=${12},idle=${13}" | socat -t 0 - UDP:localhost:8094
		;;
	esac
    done &
    # Redundant stats copy on the local FS, if not needed, comment line below 
    mpstat -P ALL -u -I ALL ${SAMPLE_TIME} ${SAMPLES} > ${RUN_DIR}/mpstat.out &
         
    sar -HB -I ALL ${SAMPLE_TIME} ${SAMPLES} | grep --line-buffered -v "Linux" | grep --line-buffered "[0-9]\.[0-9][0-9]" | while read line; do 
	set -- $line;
	Fields=$#;
	case "$Fields" in

		11) echo "lin.Gueststats.Memory,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE KBPginPerSec=$3,KBPgoutPerSec=$4,PgFltsPerSec=$5,MajPgFltsPerSec=$6,PgFrdPerSec=$7,PgScankPerSec=$8,PgScandPerSec=$9,PgStealPerSec=${10},PcntrVMEff=${11}" | socat -t 0 - UDP:localhost:8094
		;;
		5) echo "lin.Gueststats.Memory,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE KBHugeFree=$3,KBHugeUsed=$4,PcntHugeUsed=$5" | socat -t 0 - UDP:localhost:8094
		;;
		3) echo "lin.Gueststats.Interrupts.IntrIDBrkDown,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE,IntrID=$2 IntrPerSec=$3" | socat -t 0 - UDP:localhost:8094
		;;
	esac
    done &
    
    sar -m CPU -P ALL -q ${SAMPLE_TIME} ${SAMPLES} | grep --line-buffered -v "Linux" | grep --line-buffered "[0-9]\.[0-9][0-9]" | while read line; do 
	set -- $line;
	Fields=$#;
	case "$Fields" in

		4) echo "lin.Gueststats.Interrupts.IntrIDBrkDown,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE,cpu=$3 FreqMHz=$4" | socat -t 0 - UDP:localhost:8094
		;;
		8) echo "lin.Gueststats.System.Processes,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE RunQSize=$3,plistSize=$4,LdAvg1=$5,LdAvg5=$6,LdAvg15=$7,Blocked=$8" | socat -t 0 - UDP:localhost:8094
		;;
	esac
    done &

    sar -w ${SAMPLE_TIME} ${SAMPLES} | grep --line-buffered -v "Linux" | grep --line-buffered "[0-9]\.[0-9][0-9]" | while read line; do 
	set -- $line;
	Fields=$#;
	case "$Fields" in

		4) echo "lin.Gueststats.System.Processes,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE ProcCreatedPerSec=$3" | socat -t 0 - UDP:localhost:8094
		;;
	esac
    done &

    sar -WS ${SAMPLE_TIME} ${SAMPLES} | grep --line-buffered -v "Linux" | grep --line-buffered "[0-9]\.[0-9][0-9]" | while read line; do 
	set -- $line;
	Fields=$#;
	case "$Fields" in

		4) echo "lin.Gueststats.Memory,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE PgSwapInPerSec=$3,PgSwapOutPerSec=$4" | socat -t 0 - UDP:localhost:8094
		;;
		7) echo "lin.Gueststats.Memory,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE KBSwpFree=$3,KBSwpUsed=$4,PcntSwpUsed=$5,KBSwpCached=$6,PcntSwpCached=$7" | socat -t 0 - UDP:localhost:8094
		;;
	esac
    done &

    sar -v ${SAMPLE_TIME} ${SAMPLES} | grep --line-buffered -v "Linux" | grep --line-buffered -v "dentunusd" | grep "[0-9]" | while read line; do 
	set -- $line;
	Fields=$#;
	case "$Fields" in

		6) echo "lin.Gueststats.System.Files,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE DirUnusdCachEnts=$3,FileHndsUsed=$4,InodeHndsUsed=$5,NumPseudoTerm=$6" | socat -t 0 - UDP:localhost:8094
		;;
	esac
    done &

    sar -r ${SAMPLE_TIME} ${SAMPLES} | grep --line-buffered -v "Linux" | grep --line-buffered "[0-9]\.[0-9][0-9]" | while read line; do 
	set -- $line;
	Fields=$#;
	case "$Fields" in

		5) echo "lin.Gueststats.Memory,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE MemPgsFrdPerSec=$3,AddlMemPgsBufPerSec=$4,AddlMemPgsCachedPerSec=$5" | socat -t 0 - UDP:localhost:8094
		;;
		12) echo "lin.Gueststats.Memory,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE KBMemFree=$3,KBMemUsed=$4,PcntMemUsed=$5,KBBuffers=$6,KBCached=$7,KBCommit=$8,PrctCommit=$9,KBActive=${10},KBInActive=${11},KBDirty=${12}" | socat -t 0 - UDP:localhost:8094
		;;
	esac
    done &

    sar -n DEV,EDEV,NFS,NFSD ${SAMPLE_TIME} ${SAMPLES} | grep --line-buffered -v "Linux" | grep --line-buffered -v "Average" |grep --line-buffered "[0-9]\.[0-9][0-9]" | while read line; do 
	set -- $line;
	Fields=$#;
	case "$Fields" in

		10) echo "lin.Gueststats.Network,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE,IFACE=$3 RxPckPerSec=$4,TxPckPerSec=$5,RxKBPerSec=$6,TxKBPerSec=$7,RxCmpPckPerSec=$8,TxCmpPckPerSec=$9,RxMcstPckPerSec=${10}" | socat -t 0 - UDP:localhost:8094
		;;
		12) echo "lin.Gueststats.Network,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE,IFACE=$3 RxErrPerSec=$4,TxErrPerSec=$5,CollsPerSec=$6,RxDropPerSec=$7,TxDropPerSec=$8,TxCarrErrPerSec=$9,RxFramErrPerSec=${10},RxFifoErrPerSec=${11},TxFifoErrPerSec=${12}" | socat -t 0 - UDP:localhost:8094
		;;
		8) echo "lin.Gueststats.Network.NFSClient.RPC,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE CallPerSec=$3,ReTransPerSec=$4,ReadPerSec=$5,WritePerSec=$6,AccessCallsPersec=$7,GetattCallsPerSec=$8" | socat -t 0 - UDP:localhost:8094
		;;
		13) echo "lin.Gueststats.Network.NFSServer,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE RPCCallPerSec=$3,BadRPCCallPerSec=$4,NtwPckRxPerSec=$5,UdpPckRxPerSec=$6,TcpPckRxPerSec=$7,RplyCacheHitPerSec=$8,ReplyCacheMissPerSec=$9,RdRPCRxPerSec=${10},WrRPCRxPerSec=${11},AccessRPCRxPerSec=${12},GetattRPCRxPerSec=${13}" | socat -t 0 - UDP:localhost:8094
		;;
	esac
    done &
 #Test from here   
    sar -n SOCK,SOCK6 ${SAMPLE_TIME} ${SAMPLES} | grep --line-buffered -v "Linux" | grep --line-buffered -v "Average" | grep --line-buffered -v "tcpsck" | grep --line-buffered -v "tcp6sck" | grep --line-buffered "[0-9]" | while read line; do 
	set -- $line;
	Fields=$#;
	case "$Fields" in

		8) echo "lin.Gueststats.Network.Socket,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE TotScks=$3,TcpScks=$4,UdpScks=$5,RawScks=$6,NumIpFrags=$7,TcpScksTmW=$8" | socat -t 0 - UDP:localhost:8094
		;;
		6) echo "lin.Gueststats.Network.Socket,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE Tcp6Scks=$3,Udp6Scks=$4,Raw6Scks=$5,NumIp6Frags=$6" | socat -t 0 - UDP:localhost:8094
		;;
	esac
    done &
    
    sar -n IP,ICMP,EICMP ${SAMPLE_TIME} ${SAMPLES} | grep --line-buffered -v "Linux" | grep --line-buffered -v "Average" |grep --line-buffered "[0-9]\.[0-9][0-9]" | while read line; do 
	set -- $line;
	Fields=$#;
	case "$Fields" in

		10) echo "lin.Gueststats.Network.IPV4,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE IpInReceivesPerSec=$3,ForwDatagramsPerSec=$4,InDeliversPerSec=$5,OutRequestsPerSec=$6,ReasmReqdsPerSec=$7,ReasmOKsPerSec=$8,FragOKsPerSec=$9,FragCreatesPerSec=${10}" | socat -t 0 - UDP:localhost:8094
		;;
		16) echo "lin.Gueststats.Network.ICMP,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE InMsgsPerSec=$3,OutMsgsPerSec=$4,InEchosPerSec=$5,InEchoRepsPerSec=$6,OutEchosPerSec=$7,OutEchoRepsPerSec=$8,InTimestampsPerSec=$9,InTimestampRepsPerSec=${10},OutTimestampsPerSec=${11},OutTimestampRepsPerSec=${12},InAddrMasksPerSec=${13},InAddrMaskRepsPerSec=${14},OutAddrMasksPerSec=${15},OutAddrMaskRepsPerSec=${16}" | socat -t 0 - UDP:localhost:8094
		;;
		14) echo "lin.Gueststats.Network.ICMP,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE InErrorsPerSec=$3,OutErrorsPerSec=$4,InDestUnreachsPerSec=$5,OutDestUnreachsPerSec=$6,InTimeExcdsPerSec=$7,OutTimeExcdsPerSec=$8,InParmProbsPerSec=$9,OutParmProbsPerSec=${10},InSrcQuenchsPerSec=${11},OutSrcQuenchsPerSec=${12},InRedirectsPerSec=${13},OutRedirectsPerSec=${14}" | socat -t 0 - UDP:localhost:8094
		;;
	esac
    done &

    sar -n EIP,TCP,ETCP ${SAMPLE_TIME} ${SAMPLES} | grep --line-buffered -v "Linux" | grep --line-buffered -v "Average" |grep --line-buffered "[0-9]\.[0-9][0-9]" | while read line; do 
	set -- $line;
	Fields=$#;
	case "$Fields" in

		10) echo "lin.Gueststats.Network.IPV4,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE InHdrErrorsPerSec=$3,InAddrErrorsPerSec=$4,InUnknownProtosPerSec=$5,InDiscardsPerSec=$6,OutDiscardsPerSec=$7,OutNoRoutesPerSec=$8,ReasmFailsPerSec=$9,FragFailsPerSec=${10}" | socat -t 0 - UDP:localhost:8094
		;;
		6) echo "lin.Gueststats.Network.TCP,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE ActiveOpensPerSec=$3,PassiveOpensPerSec=$4,InSegsPerSec=$5,OutSegsPerSec=$6" | socat -t 0 - UDP:localhost:8094
		;;
		7) echo "lin.Gueststats.Network.ICMP,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE AttemptFailsPerSec=$3,EstabResetsPerSec=$4,RetransSegsPerSec=$5,InErrsPerSec=$6,OutRstsPerSec=$7" | socat -t 0 - UDP:localhost:8094
		;;
	esac
    done &

    sar -n UDP,IP6,EIP6,ICMP6 ${SAMPLE_TIME} ${SAMPLES} | grep --line-buffered -v "Linux" | grep --line-buffered -v "Average" |grep --line-buffered "[0-9]\.[0-9][0-9]" | while read line; do 
	set -- $line;
	Fields=$#;
	case "$Fields" in

		6) echo "lin.Gueststats.Network.UDP,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE InDatagramsPerSec=$3,OutDatagramsPerSec=$4,NoPortsPerSec=$5,InErrorsPerSec=$6" | socat -t 0 - UDP:localhost:8094
		;;
		12) echo "lin.Gueststats.Network.IPV6,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE IfStatsInReceivesPerSec=$3,IfStatsOutForwDatagramsPerSec=$4,IfStatsInDeliversPerSec=$5,IfStatsOutRequestsPerSec=$6,IfStatsReasmReqdsPerSec=$7,IfStatsReasmOKsPerSec=$8,IfStatsInMcastPktsPerSec=$9,IfStatsOutMcastPktsPerSec=${10},IfStatsOutFragOKsPerSec=${11},IfStatsOutFragCreatesPerSec=${12}" | socat -t 0 - UDP:localhost:8094
		;;
		13) echo "lin.Gueststats.Network.IPV6,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE IfStatsInHdrErrorsPerSec=$3,IfStatsInAddrErrorsPerSec=$4,IfStatsInUnknownProtosPerSec=$5,IfStatsInTooBigErrorsPerSec=$6,IfStatsInDiscardsPerSec=$7,IfStatsOutDiscardsPerSec=$8,IfStatsInNoRoutesPerSec=$9,IfLclStatsInNoRoutesPerSec=${10},IfStatsReasmFailsPerSec=${11},IfStatsOutFragFailsPerSec=${12},IfStatsInTruncatedPktsPerSec=${13}" | socat -t 0 - UDP:localhost:8094
		;;
		19) echo "lin.Gueststats.Network.ICMP6,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE IfIcmpInMsgsPerSec=$3,IfIcmpOutMsgsPerSec=$4,IfIcmpInEchosPerSec=$5,IfIcmpInEchoRepliesPerSec=$6,IfIcmpOutEchoRepliesPerSec=$7,IfIcmpInGroupMembQueriesPerSec=$8,IfIcmpInGroupMembResponsesPerSec=$9,IfIcmpOutGroupMembResponsesPerSec=${10},IfIcmpInGroupMembReductionsPerSec=${11},IfIcmpOutGroupMembReductionsPerSec=${12},IfIcmpInRouterSolicitsPerSec=${13},IfIcmpOutRouterSolicitsPerSec=${14},IfIcmpInRouterAdvertisementsPerSec=${15},IfIcmpInNeighborSolicitsPerSec=${16},IfIcmpOutNeighborSolicitsPerSec=${17},IfIcmpInNeighborAdvertisementsPerSec=${18},IfIcmpOutNeighborAdvertisementsPerSec=${19}" | socat -t 0 - UDP:localhost:8094
		;;
	esac
    done &

    sar -n UDP6,EICMP6 ${SAMPLE_TIME} ${SAMPLES} | grep --line-buffered -v "Linux" | grep --line-buffered -v "Average" |grep --line-buffered "[0-9]\.[0-9][0-9]" | while read line; do 
	set -- $line;
	Fields=$#;
	case "$Fields" in

		6) echo "lin.Gueststats.Network.UDP6,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE InDatagramsPerSec=$3,OutDatagramsPerSec=$4,NoPortsPerSec=$5,InErrorsPerSec=$6" | socat -t 0 - UDP:localhost:8094
		;;
		13) echo "lin.Gueststats.Network.ICMP6,app_name_descr=$DESC,runtag=$RUNTAG,identity=$NAME,source=$SOURCE IfIcmpInErrorsPerSec=$3,IfIcmpInDestUnreachsPerSec=$4,IfIcmpOutDestUnreachsPerSec=$5,IfIcmpInTimeExcdsPerSec=$6,IfIcmpOutTimeExcdsPerSec=$7,IfIcmpInParmProblemsPerSec=$8,IfIcmpOutParmProblemsPerSec=$9,IfIcmpInRedirectsPerSec=${10},IfIcmpOutRedirectsPerSec=${11},IfIcmpInPktTooBigsPerSec=${12},IfIcmpOutPktTooBigsPerSec=${13}" | socat -t 0 - UDP:localhost:8094
		;;
	esac
    done &
    # Redundant stats copy on the local FS, if not needed, comment line below 
    sar -A -o ${RUN_DIR}/sar.bin ${SAMPLE_TIME} ${SAMPLES} > /dev/null 2>&1 &

    date > ${RUN_DIR}/numastat.out
    #GK TBD - install NUMAstat on template
    #numastat >> ${RUN_DIR}/numastat.out
    if test $PERF -eq 1; then
	(
	PERF_SAMPLE_TIME=`expr $STDY_STATE_SEC / 2 `
	# Make sure we finish a few seconds before the very end
	PERF_SLEEP_TIME=`expr $STDY_STATE_SEC \* 4 / 10 - 5`
	sleep $PERF_SLEEP_TIME
	perf record -s -a -D -o ${RUN_DIR}/perf.during -g --stat sleep $PERF_SAMPLE_TIME
	) &
    fi

wait
date >> ${RUN_DIR}/netstat.out
netstat -s >> ${RUN_DIR}/netstat.out
date >> ${RUN_DIR}/interrupts
cat /proc/interrupts >> ${RUN_DIR}/interrupts
sar -A -f ${RUN_DIR}/sar.bin > ${RUN_DIR}/sar.txt
for f in ${RUN_DIR}/netstat.out ${RUN_DIR}/iostat.out ${RUN_DIR}/vmstat.out ${RUN_DIR}/sar.bin ${RUN_DIR}/mpstat.out ${RUN_DIR}/sar.txt; do
  gzip -9 $f
done
