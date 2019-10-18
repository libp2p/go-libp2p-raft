FROM golang:1.13-stretch AS builder
MAINTAINER Hector Sanjuan <hector@protocol.ai>

# This dockerfile builds and runs dbserver.

ENV SRC_PATH   /opt
ENV GO111MODULE on

COPY . $SRC_PATH
WORKDIR $SRC_PATH
RUN go build
RUN cp $SRC_PATH/dbserver /usr/local/bin/dbserver

ENV PORT 6666
ENTRYPOINT ["/usr/local/bin/dbserver"]
CMD []
