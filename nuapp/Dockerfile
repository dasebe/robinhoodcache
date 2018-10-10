# Set the base image to Ubuntu
FROM base/archlinux 
# File Author / Maintainer
MAINTAINER Ben Berg

ADD mirrorlist /etc/pacman.d/

RUN pacman -Syy
RUN pacman -S --noconfirm memcached python2 python2-pip git curl go socat net-tools emacs-nox

RUN ln -s /usr/bin/python2 /usr/bin/python

ADD ./src /nuapp/src
ADD ./start.sh /nuapp/start.sh

WORKDIR /nuapp

# src has to be subdir
RUN GOPATH=$(pwd) go get appserver
RUN GOPATH=$(pwd) go build appserver


# EXPOSE 27001-27064
EXPOSE 80
RUN pip2 install -r /nuapp/src/requirements.txt

CMD ./start.sh
