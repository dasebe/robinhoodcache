#! /bin/bash
if [[ $DEBUG ]]
then
    sleep 20d
fi
cd fback
go-wrapper run &> /logs/fback_$(date +%Y%m%d-%H%M%S)
