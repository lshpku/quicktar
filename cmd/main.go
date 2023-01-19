package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	ctr "github.com/lshpku/quicktar"
)

var helpMsg = `
Options:
    -h, --help            Print this help and exit.
    -c, --create          Create a new archive.
    -x, --extract         Extract the archive.
    -t, --list            List files in the archive.
    -f, --file <str>      Set the archive file.
    -v, --verbose         Verbosely list files processed.
    -1, -2, -3            Set encryption level (default none).
    -p, --password <str>  Set password.
`

func printHelpAndExit() {
	fmt.Println("usage:", os.Args[0], "[option...] [file]...")
	fmt.Println(helpMsg)
	os.Exit(0)
}

func fatalWithUsage(msg string) {
	println(msg)
	println("Type '" + os.Args[0] + " -h' for more help.")
	os.Exit(-1)
}

func fatal(msg string) {
	println(msg)
	os.Exit(-1)
}

func main() {
	mode := ""
	archive := (*string)(nil)
	verbose := false
	level := 0
	password := (*string)(nil)
	files := make([]string, 0)

	// Helper functions for parsing arguments
	i := 1
	argc := len(os.Args)
	shift := func(name string) string {
		if i+1 == argc {
			fatalWithUsage("expected value after " + name)
		}
		i++
		return os.Args[i]
	}
	once := func(old *string, repl string, name string) *string {
		if old != nil {
			fatalWithUsage(name + " is defined more than once")
		}
		return &repl
	}

	for ; i < argc; i++ {
		arg := os.Args[i]

		// Long options
		if strings.HasPrefix(arg, "--") {
			switch arg[2:] {
			case "help":
				printHelpAndExit()
			case "create", "extract", "list":
				if mode != "" {
					fatalWithUsage("ambiguous operation")
				}
				mode = arg[2:]
			case "file":
				archive = once(archive, shift(arg), "file")
			case "verbose":
				verbose = true
			case "password":
				password = once(password, shift(arg), "password")
			default:
				fatalWithUsage("unknown option: " + arg)
			}
			continue
		}

		// Short options
		if strings.HasPrefix(arg, "-") {
			nargs := len(arg)
			for j := 1; j < nargs; j++ {
				switch arg[j : j+1] {
				case "h":
					printHelpAndExit()
				case "c", "x", "t":
					if mode != "" {
						fatalWithUsage("ambiguous operation")
					}
					mode = arg[j : j+1]
				case "1", "2", "3":
					if level != 0 {
						fatalWithUsage("ambiguous level")
					}
					level = int(arg[j]) - '0'
				case "v":
					verbose = true
				case "f":
					if j+1 == nargs {
						archive = once(archive, shift("-f"), "file")
					} else {
						archive = once(archive, arg[j+1:], "file")
						j = nargs
					}
				case "p":
					if j+1 == nargs {
						password = once(password, shift("-p"), "password")
					} else {
						password = once(password, arg[j+1:], "password")
						j = nargs
					}
				default:
					fatalWithUsage("unknown option: -" + arg[j:j+1])
				}
			}
			continue
		}

		// Standalone arguments
		files = append(files, arg)
	}

	// Check options
	if mode == "" {
		fatalWithUsage("requires operation")
	}
	if archive == nil {
		fatalWithUsage("requires archive")
	}

	cpr := ctr.Store
	if level != 0 {
		if password == nil {
			fatalWithUsage("requires password on encryption")
		}
		cpr = ctr.NewCipher(level, []byte(*password))
	}

	// Create archive
	if mode == "c" || mode == "create" {
		w, err := ctr.NewWriter(*archive, cpr)
		if err != nil {
			fatal(err.Error())
		}
		defer w.Close()

		visit := func(path string, fi fs.FileInfo, err error) error {
			if err != nil {
				return nil
			}

			// Filter file
			mode := fi.Mode() & fs.ModeType
			if mode != 0 && mode != fs.ModeDir {
				return nil
			}
			baseName := ctr.BaseName(path)
			if baseName == ".DS_Store" {
				return nil
			}
			if strings.HasPrefix(baseName, "._") {
				return nil
			}

			if verbose {
				name := path
				if fi.IsDir() {
					name += "/"
				}
				fmt.Println(name)
			}

			// Create entry in archive
			w, err := w.CreateFile(path, fi.Mode(), fi.ModTime())
			if err != nil {
				return err
			}
			defer w.Close()

			if fi.IsDir() {
				return nil
			}

			// Copy content
			r, err := os.Open(path)
			if err != nil {
				return err
			}
			defer r.Close()

			_, err = io.Copy(w, r)
			return err
		}

		for _, root := range files {
			err := filepath.Walk(root, visit)
			if err != nil {
				fatal(err.Error())
			}
		}
	}

	// Extract archive
	if mode == "x" || mode == "extract" {
		fatal("not implemented")
	}

	// List archive
	if mode == "t" || mode == "list" {
		r, err := ctr.OpenReader(*archive, cpr)
		if err != nil {
			fatal(err.Error())
		}
		maxSize := int64(0)
		for _, f := range r.File {
			if f.Size() > maxSize {
				maxSize = f.Size()
			}
		}
		sizeLen := len(strconv.FormatInt(maxSize, 10))

		for _, f := range r.File {
			name := f.Name
			if f.IsDir() {
				name += "/"
			}
			if !verbose {
				fmt.Println(name)
				continue
			}
			mode := f.Mode().String()
			modified := f.ModTime().Format("2006/01/02 15:04")
			fmt.Printf("%s %*d %s %s\n", mode, sizeLen, f.Size(), modified, name)
		}
	}
}
