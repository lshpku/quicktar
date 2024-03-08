package main

import (
	"fmt"
	"os"
	"strings"

	ctr "github.com/lshpku/quicktar"
)

var helpMsg = `
Options:
    -h, --help            Print this help and exit.
    -c, --create          Create a new archive.
    -a, --append          Append to an existing archive.
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
	os.Exit(1)
}

func fatal(msg string) {
	println(msg)
	os.Exit(1)
}

var (
	flagMode    string
	flagPath    *string
	flagVerbose bool
	flagEnc     int
	flagPwd     []byte
	flagFiles   = make([]string, 0)
)

func main() {
	// Helper functions for parsing arguments
	i := 1
	var pwd *string
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
			case "create", "append", "extract", "list":
				if flagMode != "" {
					fatalWithUsage("ambiguous operation")
				}
				flagMode = arg[2:]
			case "file":
				flagPath = once(flagPath, shift(arg), "file")
			case "verbose":
				flagVerbose = true
			case "password":
				pwd = once(pwd, shift(arg), "password")
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
				case "c", "a", "x", "t":
					if flagMode != "" {
						fatalWithUsage("ambiguous operation")
					}
					flagMode = arg[j : j+1]
				case "1", "2", "3":
					if flagEnc != 0 {
						fatalWithUsage("ambiguous level")
					}
					flagEnc = int(arg[j]) - '0'
				case "v":
					flagVerbose = true
				case "f":
					if j+1 == nargs {
						flagPath = once(flagPath, shift("-f"), "file")
					} else {
						flagPath = once(flagPath, arg[j+1:], "file")
						j = nargs
					}
				case "p":
					if j+1 == nargs {
						pwd = once(pwd, shift("-p"), "password")
					} else {
						pwd = once(pwd, arg[j+1:], "password")
						j = nargs
					}
				default:
					fatalWithUsage("unknown option: -" + arg[j:j+1])
				}
			}
			continue
		}

		// Standalone arguments
		flagFiles = append(flagFiles, arg)
	}

	// Check options
	if flagMode == "" {
		fatalWithUsage("requires operation")
	}
	if flagPath == nil {
		fatalWithUsage("requires archive")
	}

	if flagEnc != ctr.EncNone {
		if pwd == nil {
			fatalWithUsage("requires password on encryption")
		}
		flagPwd = []byte(*pwd)
	}

	// Apply operation
	switch flagMode {
	case "c", "create":
		create(false)
	case "a", "append":
		create(true)
	case "x", "extract":
		// extract()
	case "t", "list":
		list()
	}
}
