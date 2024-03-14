package main

import (
	"fmt"
	"strconv"

	ctr "github.com/lshpku/quicktar"
)

func list() {
	// Open reader
	cpr := ctr.NewCipher(flagEnc, flagPwd)
	r, err := ctr.OpenReader(*flagPath, cpr)
	if err != nil {
		fatal(err.Error())
	}

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
