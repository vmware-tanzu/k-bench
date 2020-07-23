#!/bin/sh 
dir=`dirname $0`;
esxtop_op=`esxtop -b -n 1`

UTIL_FIELD=`echo "$esxtop_op" | head -1 | awk -F ',' '{for (i=1; i<=NF; i++) if($i ~ "Physical Cpu._Total....Util Time") print i}'`
CORE_FIELD=`echo "$esxtop_op" | head -1 | awk -F ',' '{for (i=1; i<=NF; i++) if($i ~ "Physical Cpu._Total....Core Util Time") print i}'`
PROCESSOR_FIELD=`echo "$esxtop_op" | head -1 | awk -F ',' '{for (i=1; i<=NF; i++) if($i ~ "Physical Cpu._Total....Processor Time") print i}'`
if test "X${CORE_FIELD}" = "X"; then
	echo "$esxtop_op" | awk -F ','  -v UTIL_FIELD=$UTIL_FIELD -v CORE_FIELD=$CORE_FIELD  -v PROCESSOR_FIELD=$PROCESSOR_FIELD 'BEGIN{printf("%-20s%7s %7s %7s\n", "Timestamp", "pcpu-used", "pcpu-util", "core-util")}{if(NR==1)next; printf("%-22s %9s %9s %9s\n", $1, $(PROCESSOR_FIELD), $(UTIL_FIELD), 0)}' | sed -e 's/"//g'
else
	echo "$esxtop_op" | awk -F ','  -v UTIL_FIELD=$UTIL_FIELD -v CORE_FIELD=$CORE_FIELD  -v PROCESSOR_FIELD=$PROCESSOR_FIELD 'BEGIN{printf("%-20s%7s %7s %7s\n", "Timestamp", "pcpu-used", "pcpu-util", "core-util")}{if(NR==1)next; printf("%-22s %9s %9s %9s\n", $1, $(PROCESSOR_FIELD), $(UTIL_FIELD), $(CORE_FIELD))}' | sed -e 's/"//g'
fi
