[ $# -eq 0 ] && { echo "Usage: $0 <run_tag>"; echo "Please specify run tag in the future"; }
dir=`dirname $0`;
$dir/install_configure_wavefront_linux.sh -p "localhost" -r "$1" -h "CICD-perf-host" -f "1";
