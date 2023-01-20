package main

import (
	_ "embed"
	"flag"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"sync/atomic"

	ctr "github.com/lshpku/quicktar"
	"golang.org/x/net/webdav"
)

var (
	flagAddr = flag.String("addr", ":20080", "Specify address")
	flagPwd  = flag.String("pwd", "", "Specify password")
	flagEnc  = flag.Int("enc", 0, "Specify encryption level")
)

//go:embed index.html
var indexFile []byte

func main() {
	root := NewFile()

	ctr.OpenFunc = acquireFile
	ctr.CloseFunc = releaseFile

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
	// Wrap writer
	rw := &ResponseWriter{
		ResponseWriter: w,
		method:         r.Method,
		url:            r.URL.String(),
	}
	url, err := url.QueryUnescape(rw.url)
	if err == nil {
		rw.url = url
	}

	// Route webdav and web requests
	if r.Method == "GET" {
		switch url {
		case "/":
			rw.Write(indexFile)
			return
		case "/-/diskReadBytes":
			val := atomic.LoadUint64(&diskReadBytes)
			str := strconv.FormatUint(val, 10)
			rw.Write([]byte(str))
			return
		}
	}

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

var (
	filePoolMu  = sync.Mutex{}
	fileNameMap = map[string][]*os.File{}
	fileDescMap = map[*os.File]string{}
)

func acquireFile(name string) (*os.File, error) {
	filePoolMu.Lock()
	defer filePoolMu.Unlock()

	list, ok := fileNameMap[name]
	if !ok {
		list = make([]*os.File, 0)
		fileNameMap[name] = list
	}

	// Select a random fd
	if len(list) > 0 {
		i := rand.Intn(len(list))
		f := list[i]
		list[i] = list[len(list)-1]
		fileNameMap[name] = list[:len(list)-1]
		return f, nil
	}

	// Open a new fd
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	fileDescMap[f] = name
	return f, nil
}

func releaseFile(f *os.File) error {
	filePoolMu.Lock()
	defer filePoolMu.Unlock()

	name := fileDescMap[f]
	list := fileNameMap[name]

	if len(list) >= 4 {
		delete(fileDescMap, f)
		return f.Close()
	}
	fileNameMap[name] = append(list, f)
	return nil
}
