#!/usr/bin/python3

# two backends
#  39f00c48
#  b4fbebd8

import random
import json
import sys

back1 = "39f00c48"
back2 = "b4fbebd8"
objects = [["8d4d4ec1f02020",12345],["dd6bd7c22aa542",20788],["24355000941058",34908],["12131376177422",37858],["8d424355000941",20881],["48efdeddbe76e5f60",20788],["3430884714871a984",20881], ["641d4cc4e0d96de89",398], ["dbe6fc5abbbc078f5",25514],["991459718784f945f",26109]]

tracefile = open("trace.txt", "w")

for t in range(0, int(sys.argv[1])):
    idx1 = random.randrange(len(objects))
    idx2 = random.randrange(len(objects))
    idx3 = random.randrange(len(objects))
    urls1 = [objects[idx1][0],objects[idx2][0]]
    urls2 = [objects[idx3][0]]
    sizes1 = [objects[idx1][1],objects[idx2][1]]
    sizes2 = [objects[idx3][1]]
    req={"t": t,
         "d": [
             {
                 back1: {
                     "S": sizes1,
                     "U": urls1,
                     "C": [1,1]
                     },
                 back2: {
                     "S": sizes2,
                     "U": urls2,
                     "C": [1]
                     }
                 }
             ]
         }
    json.dump(req,tracefile)
    tracefile.write("\n")
