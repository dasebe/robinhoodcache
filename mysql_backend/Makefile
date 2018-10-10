current_dir=$(shell pwd)
RPW="root"

all:
	docker build -t mysql_backend .
mysql_backend:
	docker build -t mysql_backend .

run:
	docker run -i --name mysql_backend -v mysql_volume:/bootstrap -e MYSQL_ROOT_PASSWORD=root -p 8888:3306 -t mysql_backend 

prepare:
	docker run --name mysql_backend -e MYSQL_ROOT_PASSWORD=${RPW} -p 8888:3306 --volume=${current_dir}/data:/var/lib/mysql -t mysql_backend 

stop:
	docker stop mysql_backend && docker rm mysql_backend

test:
	echo test
