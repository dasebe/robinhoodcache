#!/usr/bin/env python
import pprint
import traceback
import sys
import time
import random
import requests
import json
import os
from operator import itemgetter
from threading import Lock
from threading import Thread
from pymemcache.client.base import Client
from pymemcache.client import MemcacheUnknownError
from timeit import default_timer
from decimal import Decimal
from datetime import datetime
import threading

MB = 1024 * 1024

locks = {}

def pick_remove_slab_default(mc):
    return -1

def error(msg):
    print "ERROR: ", msg

def pick_remove_slab_random(mc):
    # don't need lock here, as called with acquired lock
    slab_stats = mc.stats("slabs")
    slab_pages = {}
    total_pages = 0
    for key in slab_stats:
        if key.endswith(":total_pages") and int(slab_stats[key]) > 1:
            slab_pages[key.split(":")[0]] = int(slab_stats[key])
            total_pages += int(slab_stats[key])
    choice = random.randrange(total_pages)
    seen_pages = 0
    for slab, pages in slab_pages.items():
        seen_pages += pages
        if seen_pages > choice or seen_pages == total_pages:
            return int(slab)
    return -1

def decrease_mem(mc):
    with locks[mc]:
        try:
            decrease_slab = pick_remove_slab_random(mc)
            if decrease_slab < 0:
                print "Skipping, no space to free"
                return 10 #decrease budget by 5
            mc.slabs("reassign", str(decrease_slab), "0")
            return 0 #don't decrease budget: successful
        except MemcacheUnknownError as e:
            if "BUSY" in str(e):
                print "BUSY", mc
                time.sleep(0.2)
                return 1 # decrease budget by 1 (do another iteration)
            else:
                mc.close()
                traceback.print_exc()
                error("Failed to free slab page")
                return 5 # decrease bydget by 2 (could be a real error)
        except Exception as e:
            mc.close()
            traceback.print_exc()
            error("Failed to free slab page")
            return 5 # decrease bydget by 2 (could be a real error)

def get_memlimit(mc):
    with locks[mc]:
        memlimit = mc.stats("settings")["maxbytes"]
    return memlimit

def get_malloced(mc):
    with locks[mc]:
        malloced = int(mc.stats("slabs")["total_malloced"])
    return malloced

malloc_stats = {}
def collect_stats(caches):
    global malloc_stats
    for name, caches in caches.items():
        limits = [get_memlimit(x) for x in caches]
        mallocs = [get_malloced(x) for x in caches]
        malloc_stats[name]= {"Limits": limits, "Mallocs": mallocs}

def send_stats(caches):
    global malloc_stats
    url = 'http://robinhood_stat_server/putmalloc'
    collect_stats(caches)
    requests.post(url, data=json.dumps(malloc_stats))

def change_memlimit(mc, delta_memlimit):
    target_memlimit_mb = (get_memlimit(mc) + delta_memlimit) / MB
    while True:
        with locks[mc]:
            # set current memlimit
            mc.cache_memlimit(target_memlimit_mb)
        # check current memlimit
        if get_memlimit(mc) == (target_memlimit_mb * MB):
            break
    # Evict memory
    if delta_memlimit < 0:
        attempts = 30
        while get_malloced(mc) > (target_memlimit_mb * MB) and attempts>0:
            spent = decrease_mem(mc)
            attempts-= spent
        if attempts<1:
            print "gave up",delta_memlimit, mc

# ratio>=1: limits / malloced
def malloc_ratio(key):
    global malloc_stats
    mal = malloc_stats[key]["Mallocs"]
    mem = malloc_stats[key]["Limits"]
    summal = 0
    summem = 0
    for idx, val in enumerate(mal):
        if val >1048576*2:
            summal += val
            summem += mem[idx]
    if summal<1:
        return 0
    return float(summem)/float(summal)

def get_stats(caches):
    r = requests.get('http://stat_server/getstats')
    stats = r.json()
    res = dict()
    for key in caches:
        if key in stats:
            res[key]=(stats[key]["2"]["p99"], stats[key]["2"]["malloced"])
        else:
            print "NOT FOUND", key
            res[key]=(0,0)
    return res

def robinhood_weight(caches, memlimits):
    stats = get_stats(caches)
    sorted_caches = []
    pcts = {}
    for key in caches:
        sorted_caches.append({"system": key,"cache": caches[key],"latency": stats[key][0], "malloced": stats[key][1]})
    # Sort by tail latency
    sorted_caches.sort(key=itemgetter("latency"))
    valid_cache_count = len([x for x in memlimits.values() if x > 30*MB])
    if valid_cache_count <= 1:
        return
    x = 1.0/valid_cache_count
    loser_found = False
    for i, v in enumerate(sorted_caches):
        key = v["system"]
        if memlimits[key] < 30*MB and not loser_found:
            pcts[key] = 0
        elif not loser_found:
            pcts[key] = 0
            loser_found = True
        elif i == len(sorted_caches)-1:
            pcts[key] = 2*x
        else:
            pcts[key] = x
    return pcts

def latency_weight(caches, memlimits):
    stats = get_stats(caches)
    sorted_caches = []
    pcts = {}
    meanlat = 0
    sumlat = 0
    countlat = 0
    for key in caches:
        sorted_caches.append({"system": key,"latency": stats[key][0]})
        sumlat += stats[key][0]
        countlat += 1
    meanlat = sumlat / float(countlat)
    threshold = meanlat * 0.9
    latabovemean = 0
    for v in sorted_caches:
        key = v["system"]
        if v["latency"] > threshold and 1.3 > malloc_ratio(key):
            latabovemean += v["latency"]
    for v in sorted_caches:
        key = v["system"]
        if v["latency"] > threshold and 1.3 > malloc_ratio(key):
            pcts[key] = v["latency"]/float(latabovemean)
        else:
            pcts[key] = 0
    return pcts

def req_rate_weight(caches, memlimits):
    r = requests.get('http://stat_server/getstats')
    res = dict()
    for key in caches:
        if key in r.json() and 1.3 > malloc_ratio(key):
            res[key]=r.json()[key]["2"]["count"]
        else:
            res[key]=0
    total = sum([x[1] for x in res.items()])
    pcts = {}
    if total == 0:
        return pcts
    for key in res:
        pcts[key] = float(res[key])/float(total)
    return pcts

def get_hit_ratio_weight(caches, memlimits):
    r = requests.get('http://stat_server/getstats')
    res = dict()
    for key in caches:
        if key in r.json() and 1.3 > malloc_ratio(key):
            res[key]=float(r.json()[key]["1"]["count"]) / float(r.json()[key]["2"]["count"])
        else:
            res[key]=0
    mean = sum([x[1] for x in res.items()])/float(len(res))
    distMean = {}
    for key in res:
        if res[key] < mean and res[key]>0:
            distMean[key] = mean/res[key]
        else:
            distMean[key] = 0
    total = sum([x[1] for x in distMean.items()])
    pcts= {}
    if total == 0:
        return pcts
    for key in distMean:
        pcts[key] = float(distMean[key])/float(total)
    return pcts

def crit_path_weight(caches, memlimits):
    r = requests.get('http://stat_server/getstats')
    res = dict()
    for key in caches:
        if key in r.json():
            res[key]=r.json()[key]["crit_count"]
            if 1.3 < malloc_ratio(key):
                res[key] = 0
        else:
            print "NOT FOUND", key
            res[key]=0
    total = sum([x[1] for x in res.items()])
    pcts = {}
    if total == 0:
        print "crit path: total=0"
        return pcts
    for key in res:
        pcts[key] = float(res[key])/float(total)
    return pcts

def crit_path_window_weight(caches, memlimits):
    r = requests.get('http://stat_server/getstats')
    res = dict()
    for key in caches:
        if key in r.json():
            res[key]=r.json()[key]["crit_count2"]
            if 1.3 < malloc_ratio(key):
                res[key] = 0
        else:
            print "NOT FOUND", key
            res[key]=0
    total = sum([x[1] for x in res.items()])
    pcts = {}
    if total == 0:
        print "crit path: total=0"
        return pcts
    for key in res:
        pcts[key] = float(res[key])/float(total)
    return pcts

def crit_path_small_window_weight(caches, memlimits):
    r = requests.get('http://stat_server/getstats')
    res = dict()
    for key in caches:
        if key in r.json():
            res[key]=r.json()[key]["crit_count3"]
            if 1.3 < malloc_ratio(key):
                res[key] = 0
        else:
            print "NOT FOUND", key
            res[key]=0
    total = sum([x[1] for x in res.items()])
    pcts = {}
    if total == 0:
        print "crit path: total=0"
        return pcts
    for key in res:
        pcts[key] = float(res[key])/float(total)
    return pcts



crit_smooth_res = dict()
def crit_smooth_weight(caches, memlimits):
    global crit_smooth_res
    r = requests.get('http://stat_server/getstats')
    new_res = dict()
    for key in caches:
        if key in r.json():
            new_res[key]=r.json()[key]["crit_count2"]
            if 1.3 < malloc_ratio(key):
                new_res[key] = 0
        else:
            print "NOT FOUND", key
            new_res[key]=0
    for key in new_res.keys():
        if key in crit_smooth_res:
            # smoothing
            if 1.3 < malloc_ratio(key):
                crit_smooth_res[key] = 0
            else:
                crit_smooth_res[key] = 0.7*crit_smooth_res[key] + 0.3*new_res[key]
        else:
            # init
            crit_smooth_res[key] = new_res[key]
    total = sum([x[1] for x in crit_smooth_res.items()])
    pcts = {}
    if total == 0:
        print "crit path: total=0"
        return pcts
    for key in crit_smooth_res:
        pcts[key] = float(crit_smooth_res[key])/float(total)
    return pcts


def crit_path_delta_weight(caches, memlimits):
    r = requests.get('http://stat_server/getstats')
    res = dict()
    for key in caches:
        if key in r.json():
            res[key] = r.json()[key]["crit_count"]
            res[key] *= r.json()[key]["2"]["hr_delta"]
            if 1.3 < malloc_ratio(key):
                res[key] = 0
        else:
            print "NOT FOUND", key
            res[key]=0
    total = sum([x[1] for x in res.items()])
    pcts = {}
    if total == 0:
        print "crit path: total=0"
        return pcts
    for key in res:
        pcts[key] = float(res[key])/float(total)
    #print pcts
    return pcts


def noupdate(caches, stepsize_percent):
    pass

def weighted(weight_fn):
    def generated(caches, stepsize_percent):
        return weighted_template(caches, stepsize_percent, weight_fn)
    return generated

def weighted_template(caches, stepsize_percent, weight_fn):
    global total_cache_bytes
    total = 0 
    deltas = {}
    memlimits = {}
    try:
        for key in caches:
            memlimit = get_memlimit(caches[key][0])
            memlimits[key] = memlimit
        try:
            path_pcts = weight_fn(caches, memlimits)
            if not path_pcts:
                print "no reallocation"
                return
        except Exception as e:
            traceback.print_exc()
            print "error, no reallocation, continuing"
            return
        # virtually steal 1% capacity from every cache (not enforce yet)
        local_total_cache_bytes = 0
        for key in caches:
            memlimit = memlimits[key]
            local_total_cache_bytes += memlimit
            min_allocation = memlimit - stepsize_percent*memlimit
            # only take cache space if more than 30MB left, as we need about 25 1MB pages at least
            if min_allocation > 30*MB:
                # round to MB-granularity
                delta = -int(stepsize_percent*memlimit/MB)*MB
                deltas[key] = delta
                total -= delta 
            else:
                deltas[key] = 0
        
        if total_cache_bytes != local_total_cache_bytes:
            print "CAPCHECK",total_cache_bytes,local_total_cache_bytes,total_cache_bytes-local_total_cache_bytes
            total += total_cache_bytes-local_total_cache_bytes
        remaining = total
        # reassign the stolen capacity proportionally to path_pcts
        for key in caches:
            delta = int(path_pcts[key]*total/MB)*MB
            deltas[key] += delta
            remaining -= delta
            # check for overallocation (i.e., allocated more bytes than stolen)
            if remaining < 0:
                print "OVERALLOCATED!", remaining
                return
        # if any bytes remain, allocate to first cache key
        if remaining > 0:
            print "un-rounding", remaining
            fk = deltas.keys()[0]
            deltas[fk] += remaining
            remaining -= remaining
        # enforce the memlimits async
        # across all the different cache partitions and all the caching servers
        lastT = Decimal(default_timer())
        threads = []
        print datetime.now()
        print "Known Memlimits:"
        pprint.pprint(memlimits)
        print "Deltas"
        pprint.pprint(deltas)
        print "Path Pcts"
        pprint.pprint(path_pcts)
        for key in deltas:
            deltaBytes = deltas[key]
            cache_list = caches[key]
            memlimit = memlimits[key]
            if deltaBytes >=0 or (deltaBytes < 0 and memlimit + deltaBytes > (30 * MB)):
                for cache in cache_list:
                    print "changing cache memlimit for", key
                    t = threading.Thread(target=change_memlimit, args=(cache, deltaBytes,))
                    threads.append(t)
                    t.start()
            else:
                print "BAD DELTA!", deltaBytes, memlimit, cache_list[0]
        for t in threads:
            t.join()
        newT = Decimal(default_timer())
        print "\ntotal change time", newT - lastT,"\n\n"

    except Exception as e:
        traceback.print_exc()
        return weighted_template(caches, stepsize_percent, weight_fn)



class Wrapper(object):

    def __init__(self, algorithm):
        algorithms = {"robinhood": weighted(robinhood_weight),
                    "latency": weighted(latency_weight),
                    "static": noupdate,
                    "crit_path": weighted(crit_path_weight),
                    "crit_path_window": weighted(crit_path_window_weight),
                    "crit_path_small_window": weighted(crit_path_small_window_weight),
                    "crit_smooth": weighted(crit_smooth_weight),
                    "crit_path_w_delta": weighted(crit_path_delta_weight),
                    "hit_ratio": weighted(get_hit_ratio_weight),
                    "req_rate": weighted(req_rate_weight)
                    }
        self.func = algorithms[algorithm]

    def run(self, caches, stepsize_percent):
        self.func(caches, stepsize_percent)

class OfflineOpt(object):

    def __init__(self, algorithm):
        self.start_time = datetime.now()
        self.spec = json.load(open("/config/{0}".format(algorithm["spec_file"])))
        self.ptr= -1
        self.ttl = 0

    def run(self, caches, stepsize_percent):
        elapsed_seconds = (datetime.now() - self.start_time).total_seconds()
        if elapsed_seconds > self.ttl and self.ptr < (len(self.spec) - 1):
            self.ptr += 1
            self.ttl = self.spec[self.ptr].pop("ttl")
            print "STAGE: ", self.ttl, "ELAPSED: ", elapsed_seconds, "PTR: ", self.ptr
            if self.ptr >= len(self.spec):
                return
            for backend in self.spec[self.ptr]:
                current_size = get_memlimit(caches[backend][0])
                delta_bytes = (self.spec[self.ptr][backend]*MB) - current_size
                try:
                    for cache in caches[backend]:
                        change_memlimit(cache, delta_bytes)
                except Exception as e:
                    traceback.print_exc()
                    return robinhood(caches, stepsize_percent)

class BubbleWrapper(object):


    def __init__(self, algorithm):
        self.start_time = datetime.now()
        self.spec = json.load(open("/config/{0}".format(algorithm["spec_file"])))
        self.algorithm = Wrapper(algorithm["strategy"])
        self.ptr= -1
        self.ttl = 0

    def run(self, caches, stepsize_percent):
        global total_cache_bytes
        self.algorithm.run(caches, stepsize_percent)
        elapsed_seconds = (datetime.now() - self.start_time).total_seconds()
        if elapsed_seconds > self.ttl and self.ptr < (len(self.spec) - 1):
            self.ptr += 1
            self.ttl = self.spec[self.ptr].pop("ttl")
            print "STAGE: ", self.ttl, "ELAPSED: ", elapsed_seconds, "PTR: ", self.ptr
            if self.ptr >= len(self.spec):
                return
            total_cache_bytes = self.spec[self.ptr]["mb"] * MB

def generate(algorithm):
    wrappers = {"offline_opt": OfflineOpt,
                "bubble_wrap": BubbleWrapper
                }
    key = algorithm["name"] if "name" in algorithm else algorithm
    wrapper = wrappers[key] if key in wrappers else Wrapper
    return wrapper(algorithm)


total_cache_bytes = 0

def connect(caches, reset=False):
    global total_cache_bytes
    locks.clear()
    while True:
        print "trying to connect"
        try:
            # read config
            with open(cacheConfig) as df:    
                data = json.load(df)
                print "Running controller with cache config", data
                for i in data:
                    name=i["Name"]
                    port=int(i["CachePort"])
                    if "CACHE_ADDR" in os.environ:
                        caches[name] = [Client((os.environ["CACHE_ADDR"],port), timeout=3)]
                    else:
                        caches[name] = [Client((x,port), timeout=3) for x in i["CacheAddr"]]
                    [x.slabs("automove", "0") for x in caches[name]]
                    for cache in caches[name]:
                        locks[cache] = Lock()
                    total_cache_bytes += i["CacheSize"] * MB
                    if reset:
                        for mc in caches[name]:
                            print "iCacheS",i["CacheSize"], get_memlimit(mc) , i["CacheSize"] * MB
                            #delta = i["CacheSize"] * MB - get_memlimit(mc) 
                            #change_memlimit(mc, delta)
            break
        except Exception as e:
            traceback.print_exc()
            time.sleep(1)


def usage():
    controller_print("USAGE:", sys.argv[0], "-i interval_mins", "-m memlimit_granularity_mb")
    sys.exit(1)

if __name__ == "__main__":
    if len(sys.argv) < 3:
        usage()

    cacheConfig = sys.argv[1]
    controllerConfig = sys.argv[2]
    caches = {}
    connect(caches, True)
    with open(controllerConfig) as df:    
        cdata = json.load(df)
        print "Running controller with controller config", cdata
        stepsize_percent = cdata["StepSizePerc"]
        interval_secs = cdata["IntervalSec"]
        algorithm = generate(cdata["Algorithm"])

    print("interval_secs",interval_secs)
    print("stepsize_percent",stepsize_percent)
            
    # Main loop
    print "Running control loop"
    def thread_loop(caches):
        try:
            send_stats(caches)
        except Exception:
            traceback.print_exc()
            print "continuing thread loop"
        time.sleep(5)
        t=Thread(target=thread_loop, args=(caches,))
        t.start()
    t = Thread(target=thread_loop, args=(caches,))
    t.start()
    if "WarmupSeconds" in cdata:
        time.sleep(cdata["WarmupSeconds"])
    while True:
        time.sleep(interval_secs)
        algorithm.run(caches, stepsize_percent)
