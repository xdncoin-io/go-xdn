.PHONY: build cluster test

default: build

build:
	go build -o bin/swarm    github.com/dnp/go-dnp/cmd/swarm
	go build -o bin/geth     github.com/dnp/go-dnp/cmd/geth
	go build -o bin/bootnode github.com/dnp/go-dnp/cmd/bootnode

cluster: build
	scripts/boot-cluster.sh

test:
	go test -v github.com/dnp/go-dnp/swarm/...
