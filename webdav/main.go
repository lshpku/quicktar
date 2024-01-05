package main

import (
	"flag"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	ctr "github.com/lshpku/quicktar"
	"golang.org/x/net/webdav"
)

var (
	flagAddr = flag.String("addr", ":20080", "Specify address")
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

	ctr.OpenFunc = acquireFile
	ctr.CloseFunc = releaseFile
	go closeFdLoop()

	root := NewFile()

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

var (
	filePoolMu  = sync.Mutex{}
	fileNameMap = map[string][]*os.File{}  // unused
	fileDescMap = map[*os.File]string{}    // all
	lastUseTime = map[*os.File]time.Time{} // unused
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
		delete(lastUseTime, f)
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

	if len(list) >= 16 {
		delete(fileDescMap, f)
		go f.Close()
		return nil
	}

	fileNameMap[name] = append(list, f)
	lastUseTime[f] = time.Now()
	return nil
}

func closeFdLoop() {
	for {
		filePoolMu.Lock()
		now := time.Now()
		for name, list := range fileNameMap {
			for i := 0; i < len(list); i++ {
				f := list[i]
				if now.Sub(lastUseTime[f]) < 1*time.Minute {
					continue
				}
				go list[i].Close()
				list[i] = list[len(list)-1]
				list = list[:len(list)-1]
				delete(fileDescMap, f)
				delete(lastUseTime, f)
			}
			fileNameMap[name] = list
		}
		filePoolMu.Unlock()
		time.Sleep(5 * time.Second)
	}
}
