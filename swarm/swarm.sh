#! /usr/bin/env bash

tag_workers (){
    for host in $(docker node ls -f "role=worker"| grep -v HOSTNAME | awk '{print $1}')
    do
        if [ "$#" == "0" ]
        then
            echo "$USAGE"
            return 1
        fi
        docker node update --label-add data=$1 $host
        shift
    done
}
tag_managers (){
    for host in $(docker node ls -f "role=manager" | grep -v HOSTNAME | awk '{print $1}')
    do
        if [ "$#" == "0" ]
        then
            echo "$USAGE"
            return 1
        fi
        docker node update --label-add data=$1 $host
        shift
    done
}
untag_hosts (){

        for host in $(docker node ls | grep -v HOSTNAME | awk '{print $1}')
        do
            docker node update --label-add data=empty $host
        done
}


untag_hosts 
tag_managers requestor stats nuapp
tag_workers nuapp nuapp nuapp nuapp nuapp nuapp nuapp nuapp nuapp nuapp nuapp nuapp nuapp nuapp nuapp b4fbebd8 b4fbebd8 b4fbebd8 b4fbebd8 b4fbebd8 b4fbebd8 b4fbebd8 b4fbebd8 63956c27  63956c27 39f00c48 39f00c48 39f00c48 d6018659 7385c12d 7385c12d 7385c12d 64c1ce15 b293d37d 9ee74b0b 9ee74b0b 9ee74b0b small ac59f41b df1794e4 e5fffc73 1289b3bb 30eaf8be 5b63fdf5 5b63fdf5 812126d3 812126d3

docker stack deploy robinhood --compose-file ${COMPOSE_FILE}
