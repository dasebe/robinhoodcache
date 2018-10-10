#! /bin/bash
eth0=$1
export CACHE_ADDR=$1
python -u controller.py /config/cache.json /config/controller.json ${eth0} &> /logs/controller_$(date +%Y%m%d-%H%M%S)
