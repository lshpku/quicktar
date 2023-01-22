package quicktar

import (
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"os"
	"time"
)

// Reader represents an open archive for read.
type Reader struct {
	Cipher
	File []*File
	name string
}

var OpenFunc = os.Open
var CloseFunc = func(f *os.File) error { return f.Close() }

// OpenReader opens the archive for read.
func OpenReader(name string, cipher Cipher) (*Reader, error) {
	fd, err := OpenFunc(name)
	if err != nil {
		return nil, err
	}
	defer CloseFunc(fd)

	files, err := readHeader(fd, cipher, nil)
	if err != nil {
		return nil, err
	}

	reader := &Reader{
		Cipher: cipher,
		File:   files,
		name:   name,
	}
	return reader, nil
}

func readHeader(fd *os.File, cipher Cipher, metaOff *int64) ([]*File, error) {
	// Read last block
	fi, err := fd.Stat()
	if err != nil {
		return nil, err
	}
	size := fi.Size()

	buf := make([]byte, 32)
	_, err = fd.ReadAt(buf, size-32)
	if err != nil && err != io.EOF {
		return nil, err
	}
	cipher.xorKeyStream(buf, buf, size-32)
	if binary.LittleEndian.Uint64(buf[24:]) != 0 {
		return nil, errors.New("reserved fields should be zero")
	}

	off := int64(binary.LittleEndian.Uint64(buf))
	count := int(binary.LittleEndian.Uint64(buf[8:]))
	if metaOff != nil {
		*metaOff = off
	}

	// Read file headers except names
	buf = make([]byte, size-32-off)
	_, err = fd.ReadAt(buf, off)
	if err != nil {
		return nil, err
	}
	cipher.xorKeyStream(buf, buf, off)

	files := make([]*File, count)
	for i := 0; i < count; i++ {
		offset := binary.LittleEndian.Uint64(buf)
		size := binary.LittleEndian.Uint64(buf[8:])
		mode := binary.LittleEndian.Uint32(buf[16:])
		nsec := binary.LittleEndian.Uint32(buf[20:])
		sec := binary.LittleEndian.Uint64(buf[24:])
		files[i] = &File{
			fileHeader: fileHeader{
				offset:   int64(offset),
				size:     int64(size),
				mode:     fs.FileMode(mode),
				modified: time.Unix(int64(sec), int64(nsec)),
			},
		}
		buf = buf[32:]
	}

	// Read file names
	for i := 0; i < count && len(buf) > 0; i++ {
		j := 0
		for j < len(buf) && buf[j] != 0 {
			j++
		}
		if j == len(buf) {
			return nil, errors.New("missing file names")
		}
		files[i].Name = string(buf[:j])
		files[i].fileHeader.name = BaseName(files[i].Name)
		buf = buf[j+1:]
	}

	return files, nil
}

func (r *Reader) Open(f *File) (*FileDesc, error) {
	fd, err := OpenFunc(r.name)
	if err != nil {
		return nil, err
	}
	return &FileDesc{
		Cipher: r.Cipher,
		fd:     fd,
		file:   f,
		pos:    0,
	}, nil
}

type fileHeader struct {
	// For reader, it's base name.
	// For writer, it's full name.
	name string

	offset   int64
	size     int64
	mode     fs.FileMode
	modified time.Time
}

func (f *fileHeader) Name() string       { return f.name }
func (f *fileHeader) Size() int64        { return f.size }
func (f *fileHeader) Mode() fs.FileMode  { return f.mode }
func (f *fileHeader) ModTime() time.Time { return f.modified }
func (f *fileHeader) IsDir() bool        { return f.Mode().IsDir() }
func (f *fileHeader) Sys() any           { return nil }

// File represents a file in the archive.
type File struct {
	fileHeader
	Name string // full name
}

func (f *File) FileInfo() fs.FileInfo {
	return &f.fileHeader
}

// FileDesc represents an open file for read.
type FileDesc struct {
	Cipher
	fd   *os.File
	file *File
	pos  int64
}

func (f *FileDesc) Read(p []byte) (n int, err error) {
	if f.pos == f.file.size {
		return 0, io.EOF
	}

	off := f.file.offset + f.pos
	s := off / 16 * 16
	if f.pos+int64(len(p)) > f.file.size {
		p = p[:f.file.size-f.pos]
	}
	e := (off + int64(len(p)) + 15) / 16 * 16

	buf := make([]byte, e-s)
	_, err = f.fd.ReadAt(buf, s)
	if err != nil {
		return 0, err
	}

	f.xorKeyStream(buf, buf, s)
	copy(p, buf[off%16:])
	f.pos += int64(len(p))
	return len(p), nil
}

func (f *FileDesc) Write(p []byte) (n int, err error) {
	return 0, os.ErrPermission
}

func (f *FileDesc) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		f.pos = offset
	case io.SeekCurrent:
		f.pos += offset
	case io.SeekEnd:
		f.pos = f.file.size + offset
	}
	if f.pos < 0 || f.pos > f.file.size {
		return 0, fs.ErrInvalid
	}
	return f.pos, nil
}

func (f *FileDesc) Close() error {
	return CloseFunc(f.fd)
}

func (f *FileDesc) Stat() (fs.FileInfo, error) {
	return f.file.FileInfo(), nil
}
