#! /bin/bash
echo $1
REPO=

tag_and_push (){
    image=$1
    image_dir=${2:-$image}
    echo "image: "$image
    pushd $image_dir
    export GOPATH=$(pwd)
    make $image
    status=$?
    docker tag $image $REPO/$image
    docker push $REPO/$image
    popd
#    ./ecr_policy.sh $1 $region
    return $?
}
if [[ ! $1 ]]
then
    echo "pushing all"
    tag_and_push fback go || exit 1
    tag_and_push requestor go || exit 1
    tag_and_push stat_server go || exit 1
    tag_and_push monitor || exit 1
    tag_and_push nuapp || exit 1
    tag_and_push mysql_backend || exit 1

else
    echo "push single"
    tag_and_push $1 $2
fi
