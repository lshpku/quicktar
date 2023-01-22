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

func create(archive string, cpr ctr.Cipher, verbose bool, files []string) {
	w, err := ctr.NewWriter(archive, cpr)
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
