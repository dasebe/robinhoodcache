#! /bin/bash
if [[ $DEBUG ]]
then
    sleep 20d
elif [[ ! $SKIP_UNPACK ]]
then
    sleep 10m
fi
cd statserver
if [[ $VERBOSE -eq 1 ]]
then
    FLAGS="$FLAGS -verbose"
fi
go-wrapper run $FLAGS &> /logs/stat_server_$(date +%Y%m%d-%H%M%S)
