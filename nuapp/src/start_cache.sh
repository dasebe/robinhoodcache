#! /bin/bash
eth0=$1
logdir="/logs/cache_$(date +%Y%m%d_%H%M%S)"
mkdir $logdir
./socat_server.sh ${eth0} &
python parseCacheConfig.py /config/cache.json $logdir ${eth0} # gives permission denied
