FROM golang:1.13

WORKDIR $GOPATH/src/github.com/pegnet/pegnet-node

COPY . .

ARG GOOS=linux
ENV GO111MODULE=on

RUN mkdir -p /root/.pegnet/
COPY defaultconfig.ini /root/.pegnet/defaultconfig.ini

RUN go get
RUN go build

ENTRYPOINT [ "./pegnet-node" ]