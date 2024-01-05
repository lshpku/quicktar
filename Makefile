
ifdef RELEASE
LDFLAGS += -ldflags "-s -w"
endif

target := cmd/main webdav/main

all: $(target)

cmd/main: *.go cmd/*.go
	go build $(LDFLAGS) -o $@ cmd/*.go

webdav/main: *.go webdav/*.go
	go build $(LDFLAGS) -o $@ webdav/*.go

install: $(target)
	cp cmd/main ~/bin/qct-cmd
	cp webdav/main ~/bin/qct-webdav

clean:
	rm -f $(target)
