#! /bin/bash

#the URL of the docker registry you will be using
export REPO=

#config name
export CONFIG=

# the URL of the data which will populate mysql backends
export MYSQL_DATA_URL=

# the URL of the trace data
export TRACE_URL=

# per container debug flag (container will start, sleep, and user can login start service manually)

export DEBUG_NUAPP=
export DEBUG_MYSQL=
export DEBUG_REQUESTOR=
export DEBUG_STAT_SERVER=
export DEBUG_MONITOR=

### other debugging options

#verbose stat server output
export VERBOSE_STAT_SERVER=

# bypass all caches, send all queries to the backends
export BYPASS=

# do not unzip compressed data files
export SKIP_UNPACK=

#force redownload of data files if set to 1

#all
export DOWNLOAD=

#per backend type
export DOWNLOAD_MYSQL=
export DOWNLOAD_REQUESTOR=
#per backend
export DOWNLOAD_D6018659=
export DOWNLOAD_B4FBEBD8=
export DOWNLOAD_7385C12D=
export DOWNLOAD_B293D37D=
export DOWNLOAD_9EE74B0B=
export DOWNLOAD_39F00C48=
export DOWNLOAD_B4FBEBD8=
export DOWNLOAD_1289B3BB=
export DOWNLOAD_30EAF8BE=
