#!/bin/bash
eth0=$1
while true
do
socat \
    -v -d -d \
    TCP-LISTEN:30005,bind=${eth0},crlf,reuseaddr,fork \
    SYSTEM:"
        echo HTTP/1.1 200 OK; 
        echo Content-Type\: text/plain; 
        echo;
        echo response!;
    "
done
