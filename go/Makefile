all:
	 docker build -f ./Dockerfile_fback -t fback .
	 docker build -f ./Dockerfile_requestor -t requestor .
fback:
	 docker build -f ./Dockerfile_fback -t fback .
stat_server:
	 docker build -f ./Dockerfile_stat_server -t stat_server .
requestor:
	 docker build -f ./Dockerfile_requestor -t requestor .
run:
	 docker run --network=iso --name robinhood -t robinhood

stop:
	 docker stop robinhood && docker rm robinhood
