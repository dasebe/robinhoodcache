# RobinHood: Tail Latency-Aware Caching

RobinHood is a research caching system for application servers in large web services.
A single user request to such an application server results in multiple queries to complex, diverse backend services (databases, recommender systems, ad systems, etc.).

### Key Ideas

RobinHood dynamically partition the cache space between the different backend services and continuously optimizes the partition sizes.

RobinHood's goal is to

  - repartition cache space such as to minimize the request tail latency
  - be compatible with off-the-shelf in-memory caches (such as memcached)
  - to facilitate research into different resource allocation policies and tail latency

In our experiments (testbed source code in this repo), RobinHood is effective at reducing P99 request latency spikes.

<img src="https://raw.githubusercontent.com/dasebe/robinhoodcache/master/plots/robinhood_results.png" width=500px />


### The RobinHood Algorithm and Implementation Details

[Presentation at USENIX OSDI](https://www.usenix.org/conference/osdi18/presentation/berger)

[Paper (PDF)](https://www.usenix.org/system/files/osdi18-berger.pdf). Cite as:

    RobinHood: Tail Latency Aware Caching - Dynamic Reallocation from Cache-Rich to Cache-Poor
    Daniel S. Berger, Benjamin Berg, Timothy Zhu, Siddhartha Sen, Mor Harchol-Balter. 
    USENIX OSDI, October 2018.

## RobinHood's Source Code

To test RobinHood, we built a testbed that emulates a large webservice like xbox.com.

### Overview

The testbed consists of:

 - a request generator (to replay traces of production traffic)
 - an application server, which queries backend systems and aggregates the result (key metrics like request latency are measured here)
 - several types of backends, which are either I/O bound or CPU bound
 - a central statistics server that aggregates measurements and compiles a live view of the system performance (see below)
 
<img src="https://raw.githubusercontent.com/dasebe/robinhoodcache/master/plots/dashboard.png" width=800px />

### Source Code

The RobinHood testbed is built on top of Docker Swarm. Each of the components is a separate Docker container. The source code can be found as follows:

 - go/src/requestor/: the request generator
 - nuapp/src/: the application server, which consists of
   - nuapp/src/appserver: the main source code
   - nuapp/src/subquery: schedules, tracks, and caches queries to backend servers
   - nuapp/src/shadowcache and nuapp/src/statquery: helper libraries to debug the cache state and send information to the stats server
 - go/src/mysql_backend: mysql-based I/O-bound database backend
 - go/src/fback: source code of a CPU-bound matrix multiplication backend
 - go/src/statserver: central statistics server, which keeps the 10 million recent measurements in a ring buffer and calculates key metrics like tail percentiles
 
 ### Compiling the testbed
 
 To begin, fill in the relevant, testbed-specific information in docker_env.sh .  Then type:
    source docker_env.sh
    ./push_images.sh
to compile the testbed and push the images to the specified docker container registry.
