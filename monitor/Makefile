all:
	docker build -t monitor .

monitor:
	docker build -t monitor .

run:
	docker run --name montest -t monitor

stop:
	docker stop montest && docker rm montest
