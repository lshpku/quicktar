package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	ctr "github.com/lshpku/quicktar"
)

func create(append bool) {
	// Open writer
	var w *ctr.Writer
	var err error
	cpr := ctr.NewCipher(flagEnc, flagPwd)
	if append {
		w, err = ctr.OpenWriter(*flagPath, cpr)
	} else {
		w, err = ctr.NewWriter(*flagPath, cpr)
	}
	if err != nil {
		fatal(err.Error())
	}

	// Define traversal function
	visit := func(path string, fi fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Filter file
		mode := fi.Mode() & fs.ModeType
		if mode != 0 && mode != fs.ModeDir && mode != fs.ModeSymlink {
			fmt.Fprintf(os.Stderr, "warning: unsupported file: %s\n", path)
			return nil
		}
		baseName := ctr.BaseName(path)
		if baseName == ".DS_Store" {
			return nil
		}
		if strings.HasPrefix(baseName, "._") {
			return nil
		}

		if flagVerbose {
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

		// 1. Directory
		if fi.IsDir() {
			return nil
		}

		// 2. Symlink
		if mode == fs.ModeSymlink {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if _, err := w.Write([]byte(link)); err != nil {
				return err
			}
		}

		// 3. Regular file
		r, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, r)
		r.Close()
		return err
	}

	// Traverse files
	for _, root := range flagFiles {
		var fi fs.FileInfo
		fi, err = os.Stat(root)
		if err != nil {
			break
		}
		if fi.IsDir() {
			lastChar := rune(0)
			for _, c := range root {
				lastChar = c
			}
			if lastChar == '/' {
				root = root[:len(root)-1]
			}
		}
		err = filepath.Walk(root, visit)
		if err != nil {
			break
		}
	}

	// Close the file before raising any error, so that the archive is closed properly.
	closeErr := w.Close()
	if err != nil {
		fatal(err.Error())
	}
	if closeErr != nil {
		fatal(closeErr.Error())
	}
}
