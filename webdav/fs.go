package main

import (
	"context"
	"io/fs"
	"os"
	"sync/atomic"
	"time"

	ctr "github.com/lshpku/quicktar"
	"golang.org/x/net/webdav"
)

var diskReadBytes uint64

type FS struct {
	root *File
}

func (_ *FS) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	return os.ErrPermission
}

func (s *FS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	f := s.lookup(name)
	if f == nil {
		return nil, fs.ErrNotExist
	}
	if flag != os.O_RDONLY {
		return nil, fs.ErrPermission
	}
	fd := &FileDesc{
		file: f,
	}
	return fd, nil
}

func (_ *FS) RemoveAll(ctx context.Context, name string) error {
	return fs.ErrPermission
}

func (_ *FS) Rename(ctx context.Context, oldName, newName string) error {
	return fs.ErrPermission
}

func (s *FS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	f := s.lookup(name)
	if f == nil {
		return nil, fs.ErrNotExist
	}
	return f, nil
}

func (s *FS) lookup(path string) *File {
	if len(path) == 0 || path[0] != '/' {
		return nil
	}
	cur := s.root
	for _, s := range ctr.Split(path[1:]) {
		if s == "" {
			break
		}
		if next, ok := cur.children[s]; ok {
			cur = next
		} else {
			return nil
		}
	}
	return cur
}

type File struct {
	file     *ctr.File
	reader   *ctr.Reader
	children map[string]*File
}

func NewFile() *File {
	return &File{
		children: make(map[string]*File),
	}
}

func (f *File) Name() string {
	if f.file == nil {
		return "/"
	}
	return f.file.FileInfo().Name()
}

func (f *File) Size() int64 {
	if f.file == nil {
		return 0
	}
	return f.file.Size()
}

func (f *File) Mode() fs.FileMode {
	if f.file == nil || f.file.IsDir() {
		return fs.ModeDir | 0555
	}
	return 0444
}

func (f *File) ModTime() time.Time {
	if f.file == nil {
		return time.Unix(0, 0)
	}
	return f.file.ModTime()
}

func (f *File) IsDir() bool {
	return f.Mode().IsDir()
}

func (f *File) Sys() any { return nil }

type FileDesc struct {
	file *File
	fd   *ctr.FileDesc
}

func (f *FileDesc) init() (err error) {
	if f.fd == nil {
		f.fd, err = f.file.reader.Open(f.file.file)
	}
	return err
}

func (f *FileDesc) Read(p []byte) (n int, err error) {
	if err = f.init(); err != nil {
		return
	}
	n, err = f.fd.Read(p)
	atomic.AddUint64(&diskReadBytes, uint64(n))
	return
}

func (f *FileDesc) Write(p []byte) (n int, err error) {
	return 0, os.ErrPermission
}

func (f *FileDesc) Seek(offset int64, whence int) (int64, error) {
	if err := f.init(); err != nil {
		return 0, err
	}
	return f.fd.Seek(offset, whence)
}

func (f *FileDesc) Close() error {
	if f.fd == nil {
		return nil
	}
	return f.fd.Close()
}

func (f *FileDesc) Readdir(count int) ([]fs.FileInfo, error) {
	if count > len(f.file.children) || count <= 0 {
		count = len(f.file.children)
	}
	list := make([]fs.FileInfo, 0, count)
	for _, fi := range f.file.children {
		list = append(list, fi)
		count--
		if count == 0 {
			break
		}
	}
	return list, nil
}

func (f *FileDesc) Stat() (fs.FileInfo, error) {
	return f.file, nil
}
