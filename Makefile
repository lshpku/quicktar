ifdef RELEASE
LDFLAGS += -ldflags "-s -w"
endif

GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
GOEXT :=

ifeq ($(GOOS), windows)
GOEXT := .exe
endif

all: bin/main-$(GOOS)-$(GOARCH)$(GOEXT) bin/webdav-$(GOOS)-$(GOARCH)$(GOEXT)

bin/main-$(GOOS)-$(GOARCH)$(GOEXT): *.go cmd/*.go
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(LDFLAGS) -o $@ ./cmd

bin/webdav-$(GOOS)-$(GOARCH)$(GOEXT): *.go webdav/*.go
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(LDFLAGS) -o $@ ./webdav

clean:
	rm -rf bin
