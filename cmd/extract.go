package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"

	ctr "github.com/lshpku/quicktar"
)

func list() {
	// Open reader
	cpr := ctr.NewCipher(flagEnc, flagPwd)
	r, err := ctr.OpenReader(*flagPath, cpr)
	nilOrFatal(err)

	// Find the longest size
	maxSize := int64(0)
	for _, f := range r.File {
		if f.Size() > maxSize {
			maxSize = f.Size()
		}
	}
	sizeLen := len(strconv.FormatInt(maxSize, 10))

	// Print files
	for _, f := range r.File {
		name := f.Name
		if f.IsDir() {
			name += "/"
		}
		if !flagVerbose {
			fmt.Println(name)
			continue
		}
		mode := f.Mode().String()
		modTime := f.ModTime().Format("2006/01/02 15:04")
		fmt.Printf("%s %*d %s %s\n", mode, sizeLen, f.Size(), modTime, name)
	}
}

func extract() {
	// Open reader
	cpr := ctr.NewCipher(flagEnc, flagPwd)
	r, err := ctr.OpenReader(*flagPath, cpr)
	nilOrFatal(err)

	dirMap := map[string]*ctr.File{}
	dirLastIdx := map[string]int{}
	dirExists := map[string]bool{}

	// Find the last indices of directories
	for i, f := range r.File {
		for _, p := range ctr.Parents(f.Name) {
			dirLastIdx[p] = i
		}
		if f.IsDir() {
			dirMap[f.Name] = f
			dirLastIdx[f.Name] = i
		}
	}

	createBasedir := func(path string) {
		parents := ctr.Parents(path)
		if len(parents) == 0 {
			return
		}
		dirname := parents[len(parents)-1]
		if dirExists[dirname] {
			return
		}
		nilOrFatal(os.MkdirAll(dirname, 0755))
		for _, p := range parents {
			dirExists[p] = true
		}
	}

	for i, f := range r.File {
		mode := f.Mode() & fs.ModeType

		// 1. Regular file
		if mode == 0 {
			if flagVerbose {
				fmt.Println(f.Name)
			}

			// Create parent directories
			createBasedir(f.Name)

			// Extract file
			rf, err := r.Open(f)
			nilOrFatal(err)
			wf, err := os.OpenFile(f.Name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
			nilOrFatal(err)
			_, err = io.Copy(wf, rf)
			nilOrFatal(err)
			rf.Close()

			// Set file metadata
			nilOrFatal(wf.Chmod(f.Mode()))
			nilOrFatal(wf.Close())
			nilOrFatal(os.Chtimes(f.Name, f.ModTime(), f.ModTime()))
		}

		// 2. Symlink
		if mode == fs.ModeSymlink {
			if flagVerbose {
				fmt.Println(f.Name)
			}
			createBasedir(f.Name)
			rf, err := r.Open(f)
			nilOrFatal(err)
			data, err := io.ReadAll(rf)
			nilOrFatal(err)
			nilOrFatal(os.Symlink(string(data), f.Name))
			// Note: ignore mode or modTime for symlink
		}

		// 3. Empty directory
		if f.IsDir() && dirLastIdx[f.Name] == i {
			if flagVerbose {
				fmt.Println(f.Name + "/")
			}
			// Note: use MkdirAll to avoid 'file exists' error.
			nilOrFatal(os.MkdirAll(f.Name, 0755))
			for _, p := range ctr.Parents(f.Name) {
				dirExists[p] = true
			}
			nilOrFatal(os.Chmod(f.Name, f.Mode()))
			nilOrFatal(os.Chtimes(f.Name, f.ModTime(), f.ModTime()))
		}

		// 4. Parent directories
		for _, p := range ctr.Parents(f.Name) {
			if dirLastIdx[p] == i {
				if df, ok := dirMap[p]; ok {
					if flagVerbose {
						fmt.Println(p + "/")
					}
					nilOrFatal(os.Chmod(p, df.Mode()))
					nilOrFatal(os.Chtimes(p, df.ModTime(), df.ModTime()))
				}
			}
		}
	}
}
