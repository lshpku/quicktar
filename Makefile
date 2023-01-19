
ifdef RELEASE
LDFLAGS += -ldflags "-s -w"
endif

target := cmd/main webdav/main

all: $(target)

cmd/main: *.go cmd/*.go
	go build $(LDFLAGS) -o $@ cmd/*.go

webdav/main: *.go webdav/*.go webdav/*.html
	go build $(LDFLAGS) -o $@ webdav/*.go

clean:
	rm -f $(target)

