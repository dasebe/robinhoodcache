#!/usr/bin/python2
import json
import sys
import select
from subprocess import Popen
import subprocess
with open(sys.argv[1]) as df:    
    data = json.load(df)
logdir=sys.argv[2]
eth0 = sys.argv[3]
ports = set([])
procs = {}
for i in data:
    if i["CachePort"] not in ports:
        startcmd = ["memcached","-v", "-c", "30000", "-l", eth0, "-u", "root", "-p",str(i["CachePort"]),"-I","8388608","-m",str(i["CacheSize"]),"-R","100"]  #,"-t","8"
        print startcmd
        stdout_log = open("{0}/{1}.stdout".format(logdir, i["CachePort"]), 'w')
        stderr_log = open("{0}/{1}.stderr".format(logdir, i["CachePort"]), 'w')
        p = Popen(startcmd, stdout=stdout_log, stderr=stderr_log)
        ports.add(i["CachePort"])
#        procs[p.stderr] = i["CachePort"]
#        procs[p.stdout] = i["CachePort"]
#pfds = procs.keys()
#while True:
#    rfds, wfds, xfds = select.select(pfds, [], [])
#    for rfd in rfds:
#        print procs[rfd], rfd.readline().strip()
p.communicate()
