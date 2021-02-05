# This Makefile is meant to be used by people that do not usually work
# with Go source code. If you know what GOPATH is then you probably
# don't need to bother with make.

.PHONY: xdn android ios xdn-cross swarm evm all test clean
.PHONY: xdn-linux xdn-linux-386 xdn-linux-amd64 xdn-linux-mips64 xdn-linux-mips64le
.PHONY: xdn-linux-arm xdn-linux-arm-5 xdn-linux-arm-6 xdn-linux-arm-7 xdn-linux-arm64
.PHONY: xdn-darwin xdn-darwin-386 xdn-darwin-amd64
.PHONY: xdn-windows xdn-windows-386 xdn-windows-amd64

GOBIN = $(shell pwd)/build/bin
GO ?= latest

xdn:
	build/env.sh go run build/ci.go install ./cmd/xdn
	@echo "Done building."
	@echo "Run \"$(GOBIN)/xdn\" to launch xdn."

swarm:
	build/env.sh go run build/ci.go install ./cmd/swarm
	@echo "Done building."
	@echo "Run \"$(GOBIN)/swarm\" to launch swarm."

all:
	build/env.sh go run build/ci.go install

android:
	build/env.sh go run build/ci.go aar --local
	@echo "Done building."
	@echo "Import \"$(GOBIN)/xdn.aar\" to use the library."

ios:
	build/env.sh go run build/ci.go xcode --local
	@echo "Done building."
	@echo "Import \"$(GOBIN)/Geth.framework\" to use the library."

test: all
	build/env.sh go run build/ci.go test

clean:
	rm -fr build/_workspace/pkg/ $(GOBIN)/*

# The devtools target installs tools required for 'go generate'.
# You need to put $GOBIN (or $GOPATH/bin) in your PATH to use 'go generate'.

devtools:
	env GOBIN= go get -u golang.org/x/tools/cmd/stringer
	env GOBIN= go get -u github.com/jteeuwen/go-bindata/go-bindata
	env GOBIN= go get -u github.com/fjl/gencodec
	env GOBIN= go install ./cmd/abigen

# Cross Compilation Targets (xgo)

xdn-cross: xdn-linux xdn-darwin xdn-windows xdn-android xdn-ios
	@echo "Full cross compilation done:"
	@ls -ld $(GOBIN)/xdn-*

xdn-linux: xdn-linux-386 xdn-linux-amd64 xdn-linux-arm xdn-linux-mips64 xdn-linux-mips64le
	@echo "Linux cross compilation done:"
	@ls -ld $(GOBIN)/xdn-linux-*

xdn-linux-386:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/386 -v ./cmd/xdn
	@echo "Linux 386 cross compilation done:"
	@ls -ld $(GOBIN)/xdn-linux-* | grep 386

xdn-linux-amd64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/amd64 -v ./cmd/xdn
	@echo "Linux amd64 cross compilation done:"
	@ls -ld $(GOBIN)/xdn-linux-* | grep amd64

xdn-linux-arm: xdn-linux-arm-5 xdn-linux-arm-6 xdn-linux-arm-7 xdn-linux-arm64
	@echo "Linux ARM cross compilation done:"
	@ls -ld $(GOBIN)/xdn-linux-* | grep arm

xdn-linux-arm-5:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm-5 -v ./cmd/xdn
	@echo "Linux ARMv5 cross compilation done:"
	@ls -ld $(GOBIN)/xdn-linux-* | grep arm-5

xdn-linux-arm-6:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm-6 -v ./cmd/xdn
	@echo "Linux ARMv6 cross compilation done:"
	@ls -ld $(GOBIN)/xdn-linux-* | grep arm-6

xdn-linux-arm-7:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm-7 -v ./cmd/xdn
	@echo "Linux ARMv7 cross compilation done:"
	@ls -ld $(GOBIN)/xdn-linux-* | grep arm-7

xdn-linux-arm64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm64 -v ./cmd/xdn
	@echo "Linux ARM64 cross compilation done:"
	@ls -ld $(GOBIN)/xdn-linux-* | grep arm64

xdn-linux-mips:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mips --ldflags '-extldflags "-static"' -v ./cmd/xdn
	@echo "Linux MIPS cross compilation done:"
	@ls -ld $(GOBIN)/xdn-linux-* | grep mips

xdn-linux-mipsle:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mipsle --ldflags '-extldflags "-static"' -v ./cmd/xdn
	@echo "Linux MIPSle cross compilation done:"
	@ls -ld $(GOBIN)/xdn-linux-* | grep mipsle

xdn-linux-mips64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mips64 --ldflags '-extldflags "-static"' -v ./cmd/xdn
	@echo "Linux MIPS64 cross compilation done:"
	@ls -ld $(GOBIN)/xdn-linux-* | grep mips64

xdn-linux-mips64le:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mips64le --ldflags '-extldflags "-static"' -v ./cmd/xdn
	@echo "Linux MIPS64le cross compilation done:"
	@ls -ld $(GOBIN)/xdn-linux-* | grep mips64le

xdn-darwin: xdn-darwin-386 xdn-darwin-amd64
	@echo "Darwin cross compilation done:"
	@ls -ld $(GOBIN)/xdn-darwin-*

xdn-darwin-386:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=darwin/386 -v ./cmd/xdn
	@echo "Darwin 386 cross compilation done:"
	@ls -ld $(GOBIN)/xdn-darwin-* | grep 386

xdn-darwin-amd64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=darwin/amd64 -v ./cmd/xdn
	@echo "Darwin amd64 cross compilation done:"
	@ls -ld $(GOBIN)/xdn-darwin-* | grep amd64

xdn-windows: xdn-windows-386 xdn-windows-amd64
	@echo "Windows cross compilation done:"
	@ls -ld $(GOBIN)/xdn-windows-*

xdn-windows-386:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=windows/386 -v ./cmd/xdn
	@echo "Windows 386 cross compilation done:"
	@ls -ld $(GOBIN)/xdn-windows-* | grep 386

xdn-windows-amd64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=windows/amd64 -v ./cmd/xdn
	@echo "Windows amd64 cross compilation done:"
	@ls -ld $(GOBIN)/xdn-windows-* | grep amd64

plot:
	build/env.sh go run build/ci.go install ./cmd/plot
	@echo "Done building."
	@echo "Run \"$(GOBIN)/dplot\" to launch dplot."

plot-windows-386:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=windows/386 -v ./cmd/plot
	@echo "Windows 386 cross compilation done:"
	@ls -ld $(GOBIN)/plot-windows-* | grep 386

priv:
	build/env.sh go run build/ci.go install ./cmd/priv
	@echo "Done building."
	@echo "Run \"$(GOBIN)/priv\" to launch priv."