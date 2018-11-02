#! /bin/bash
if [[ $DEBUG ]]
then
    sleep 20d
fi
curl -L ${CONFIG_URL}/${CONFIG}.tar.gz -o ${CONFIG}.tar.gz
tar -xzvf ${CONFIG}.tar.gz
pushd /bootstrap
if [[ ! -e "./data.tar.gz" || $DOWNLOAD || $DOWNLOAD2 ]]
then
    curl -L $MYSQL_DATA_URL > ./data.tar.gz || (echo "curl failed: $MYSQL_DATA_URL" && rm data.tar.gz && exit 5)
fi
if [[ ! $SKIP_UNPACK ]]
then
    tar -zxvf ./data.tar.gz || (echo "extract failed" && rm -rf data && exit 5)
fi

pushd data
for i in $(ls)
do
    ln -s -f /bootstrap/data/$i /var/lib/mysql/$i
done
popd
popd
docker-entrypoint.sh mysqld &> /logs/mysql_$(date +%Y%m%d-%H%M%S)
