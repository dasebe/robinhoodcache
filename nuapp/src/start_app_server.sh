#! /bin/bash
eth0=$1
/nuapp/bin/appserver -cacheip ${eth0} &> /logs/app_server_$(date +%Y%m%d-%H%M%S)
