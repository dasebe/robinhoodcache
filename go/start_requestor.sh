#! /bin/bash
if [[ $DEBUG ]]
then
    sleep 20d
elif [[ ! $SKIP_UNPACK ]]
then
    sleep 10m
fi
curl -L ${CONFIG_URL}/${CONFIG}.tar.gz -o ${CONFIG}.tar.gz
tar -xzvf ${CONFIG}.tar.gz
mv config /config
pushd /bootstrap
if [[ ! -e "./trace.json" || $DOWNLOAD ]]
then
    curl -L $TRACE_URL > trace.json.tar.gz || (echo "curl failed: $TRACE_URL" && rm trace.json.tar.gz&& exit 5)
    tar -zxf /bootstrap/trace.json.tar.gz || (echo "extract failed" && rm -rf trace.json && exit 5)
fi
popd

cd requestor
go-wrapper run &> /logs/requestor_$(date +%Y%m%d-%H%M%S)

echo "done"
