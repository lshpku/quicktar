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
	File    []*File
	name    string
	fdCache *fdCache
}

// OpenReader opens the archive for read.
func OpenReader(name string, cipher Cipher) (*Reader, error) {
	fd, err := os.Open(name)
	if err != nil {
		return nil, err
	}

	files, err := readMeta(fd, &cipher, nil)
	if err != nil {
		fd.Close()
		return nil, err
	}

	reader := &Reader{
		Cipher:  cipher,
		File:    files,
		name:    name,
		fdCache: newFdCache(fd),
	}
	return reader, nil
}

// readMeta reads the meta part of fd.
// metaOff is the offset of meta, where you can append from.
// The nonce in cipher will be updated.
func readMeta(fd *os.File, cipher *Cipher, metaOff *int64) ([]*File, error) {
	// Read header
	buf := make([]byte, 32)
	if _, err := fd.Read(buf); err != nil {
		return nil, err
	}
	if string(buf[:8]) != "QuickTar" {
		if metaOff != nil {
			return nil, errors.New("bad magic")
		}
		// Deprecated, read-only
		println("warning: bad magic, fallback to older format")
		cipher.nonce = []uint64{binary.BigEndian.Uint64(deprecatedNonce), 0}
		return readHeader(fd, *cipher, nil)
	}
	metaEnd := int64(binary.LittleEndian.Uint64(buf[8:]))
	if cipher.block != nil {
		cipher.nonce = []uint64{
			binary.BigEndian.Uint64(buf[16:]),
			binary.BigEndian.Uint64(buf[24:]),
		}
	}

	// Read the final block
	if _, err := fd.ReadAt(buf, metaEnd-32); err != nil {
		return nil, err
	}
	cipher.xorKeyStream(buf, buf, metaEnd-32)
	if binary.LittleEndian.Uint64(buf[24:]) != 0 {
		return nil, errors.New("wrong password")
	}
	metaSize := int64(binary.LittleEndian.Uint64(buf))
	count := int(binary.LittleEndian.Uint64(buf[8:]))
	metaStart := metaEnd - metaSize
	if metaOff != nil {
		*metaOff = metaStart
	}

	// Read file metadata
	buf = make([]byte, metaSize-32)
	if _, err := fd.ReadAt(buf, metaStart); err != nil {
		return nil, err
	}
	cipher.xorKeyStream(buf, buf, metaStart)
	files := make([]*File, count)
	for i := 0; i < count; i++ {
		offset := binary.LittleEndian.Uint64(buf)
		size := binary.LittleEndian.Uint64(buf[8:])
		mode := binary.LittleEndian.Uint32(buf[16:])
		nsec := binary.LittleEndian.Uint32(buf[20:])
		sec := binary.LittleEndian.Uint64(buf[24:])
		files[i] = &File{
			fileHeader: fileHeader{
				offset:  int64(offset),
				size:    int64(size),
				mode:    fs.FileMode(mode),
				modTime: time.Unix(int64(sec), int64(nsec)),
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
				offset:  int64(offset),
				size:    int64(size),
				mode:    fs.FileMode(mode),
				modTime: time.Unix(int64(sec), int64(nsec)),
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

// Open opens the file for reading.
//
// User may open multiple files concurrently. If so, each file has its own fd.
func (r *Reader) Open(f *File) (*FileDesc, error) {
	fd := r.fdCache.acquire()
	if fd == nil {
		var err error
		fd, err = os.Open(r.name)
		if err != nil {
			return nil, err
		}
	}
	return &FileDesc{
		reader: r,
		fd:     fd,
		file:   f,
		pos:    0,
	}, nil
}

type fileHeader struct {
	// For reader, it's base name.
	// For writer, it's full name.
	name string

	offset  int64
	size    int64
	mode    fs.FileMode
	modTime time.Time
}

func (f *fileHeader) Name() string       { return f.name }
func (f *fileHeader) Size() int64        { return f.size }
func (f *fileHeader) Mode() fs.FileMode  { return f.mode }
func (f *fileHeader) ModTime() time.Time { return f.modTime }
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
	reader *Reader
	fd     *os.File
	file   *File
	pos    int64
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

	f.reader.xorKeyStream(buf, buf, s)
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
	return f.reader.fdCache.release(f.fd)
}

func (f *FileDesc) Stat() (fs.FileInfo, error) {
	return f.file.FileInfo(), nil
}

func (r *Reader) SetFdCacheSize(size int) {
	r.fdCache.size = size
}

func (r *Reader) SetFdCacheTimeout(timeout time.Duration) {
	r.fdCache.timeout = timeout
}
