#! /bin/bash
if [[ $DEBUG ]]
then
    sleep 20d
elif [[ ! $SKIP_UNPACK ]]
then
    sleep 10m
fi
cd monitor
go-wrapper run &> /logs/monitor_$(date +%Y%m%d-%H%M%S)
