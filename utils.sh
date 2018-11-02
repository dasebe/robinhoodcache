docker_ssh (){
    docker exec -it $(docker ps | grep $1 | awk '{print $1}') bash
}

export -f docker_ssh
