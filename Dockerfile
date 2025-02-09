FROM hub.pingcap.net/mirrors/golang:1.16 as builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . /build
RUN go build -o /schrddl


FROM hub.pingcap.net/mirrors/debian:buster

RUN apt -y update && apt -y install wget curl \
 && rm -rf /var/lib/apt/lists/*

COPY --from=builder /schrddl /usr/local/bin/schrddl

ENTRYPOINT ["/usr/local/bin/schrddl"]

# hub.pingcap.net/qa/schrddl
