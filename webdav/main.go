package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	ctr "github.com/lshpku/quicktar"
	"golang.org/x/net/webdav"
)

var (
	flagAddr = flag.String("addr", "127.0.0.1:8080", "Specify listen address")
	flagPwd  = flag.String("pwd", "", "Specify password")
	flagEnc  = flag.Int("enc", 0, "Specify encryption level")
)

func main() {
	// Parse flag
	flag.Parse()

	if *flagEnc < 0 || *flagEnc > 3 {
		println("-enc should be in range [0, 3]")
		os.Exit(-1)
	}
	cpr := ctr.NewCipher(*flagEnc, []byte(*flagPwd))

	root := NewFile()

	// Open files
	for _, name := range flag.Args() {
		log.Println("opening file", name)
		r, err := ctr.OpenReader(name, cpr)
		if err != nil {
			log.Fatal(err)
		}
		r.SetFdCacheSize(0)
		r.SetFdCacheTimeout(30 * time.Second)

		// Build directory
		for _, f := range r.File {
			cur := root
			for _, s := range ctr.Split(f.Name) {
				if s == "" {
					break
				}
				next, ok := cur.children[s]
				if !ok {
					next = NewFile()
					cur.children[s] = next
				}
				cur = next
			}
			cur.file = f
			cur.reader = r
		}
	}

	// Start server
	log.Println("listen on", *flagAddr)
	err := http.ListenAndServe(*flagAddr, &httpHandler{
		webdav.Handler{
			FileSystem: &FS{
				root: root,
			},
			LockSystem: webdav.NewMemLS(),
		},
	})
	log.Fatal(err)
}
