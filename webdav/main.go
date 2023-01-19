package main

import (
	"flag"
	"log"
	"net/http"
	"net/url"
	"os"

	ctr "github.com/lshpku/quicktar"
	"golang.org/x/net/webdav"
)

var (
	flagAddr = flag.String("addr", ":20080", "Specify address")
	flagPwd  = flag.String("pwd", "", "Specify password")
	flagEnc  = flag.Int("enc", 0, "Specify encryption level")
)

func main() {
	root := NewFile()

	// Parse flag
	flag.Parse()

	if *flagEnc < 0 || *flagEnc > 3 {
		println("-enc should be in range [0, 3]")
		os.Exit(-1)
	}
	cpr := ctr.NewCipher(*flagEnc, []byte(*flagPwd))

	// Open files
	for _, name := range flag.Args() {
		r, err := ctr.OpenReader(name, cpr)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("open file", name)

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
	log.Println("listen on :20080")

	err := http.ListenAndServe(":20080", &Handler{
		webdav.Handler{
			FileSystem: &FS{
				root: root,
			},
			LockSystem: webdav.NewMemLS(),
		},
	})

	log.Fatal(err)
}

type Handler struct {
	webdav.Handler
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rw := &ResponseWriter{
		ResponseWriter: w,
		method:         r.Method,
		url:            r.URL.String(),
	}
	url, err := url.QueryUnescape(rw.url)
	if err == nil {
		rw.url = url
	}
	log.Println(rw.method, rw.url)
	h.Handler.ServeHTTP(rw, r)
}

type ResponseWriter struct {
	http.ResponseWriter
	method string
	url    string
}

func (w *ResponseWriter) WriteHeader(statusCode int) {
	w.ResponseWriter.WriteHeader(statusCode)
	text := http.StatusText(statusCode)
	log.Println(w.method, w.url, statusCode, text)
}
