package quicktar

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"os"
	"time"
)

// Writer represents an open archive for write.
type Writer struct {
	Cipher
	fd        *os.File
	file      []*fileHeader
	fileIndex map[string]int
	pos       int64
	buf       []byte
}

// NewWriter creates a new archive for write.
func NewWriter(name string, cipher Cipher) (*Writer, error) {
	f, err := os.Create(name)
	if err != nil {
		return nil, err
	}

	// Write header
	header := make([]byte, 32)
	copy(header, []byte("QuickTar"))
	if cipher.block != nil {
		binary.BigEndian.PutUint64(header[16:], cipher.nonce[0])
		binary.BigEndian.PutUint64(header[24:], cipher.nonce[1])
	}
	if _, err = f.Write(header); err != nil {
		return nil, err
	}

	return &Writer{
		Cipher:    cipher,
		fd:        f,
		file:      make([]*fileHeader, 0),
		fileIndex: make(map[string]int),
		pos:       32,
		buf:       make([]byte, 0),
	}, nil
}

// OpenWriter opens an existing archive for append.
func OpenWriter(name string, cipher Cipher) (*Writer, error) {
	f, err := os.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	var metaOff int64
	files, err := readMeta(f, &cipher, &metaOff)
	if err != nil {
		return nil, err
	}
	_, err = f.Seek(metaOff, io.SeekStart)
	if err != nil {
		return nil, err
	}
	w := &Writer{
		Cipher:    cipher,
		fd:        f,
		file:      make([]*fileHeader, 0),
		fileIndex: make(map[string]int),
		pos:       metaOff,
		buf:       make([]byte, 0),
	}
	for _, f := range files {
		h := f.fileHeader
		h.name = f.Name
		w.fileIndex[f.Name] = len(w.file)
		w.file = append(w.file, &h)
	}
	return w, nil
}

// Create provides easy access to CreateFile.
// The name follows the same constraints as CreateFile. However, to create
// a directory instead of a file, add a trailing slash to the name.
// Default value for mode is 0666 and modified time is now.
func (w *Writer) Create(name string) (io.WriteCloser, error) {
	mode := fs.FileMode(0666)
	uName := []rune(name)
	if uName[len(uName)-1] == '/' {
		name = name[:len(name)-1]
		mode |= fs.ModeDir
	}
	return w.CreateFile(name, mode, time.Now())
}

// CreateFile creates a new file in the archive for write.
// The name should follow the following constraints:
//  1. Each level of directories is seperated by a single '/'.
//  2. No leading or trailing '/', even for directories.
//  3. For any level, an empty string, '.' or '..' is not allowed.
//
// Currently, only regular file, directory and soft link are supported.
//
// For directories, the returned file is already closed.
func (w *Writer) CreateFile(name string, mode fs.FileMode, modified time.Time) (io.WriteCloser, error) {
	// Check name
	if name == "" {
		return nil, errors.New("empty name")
	}
	if name[0] == '/' {
		return nil, errors.New("leading slash")
	}
	split := make([]string, 0)
	lastPos := 0
	for i, s := range name {
		if s == '/' {
			split = append(split, name[lastPos:i])
			lastPos = i + 1
			if i == len(name)-1 {
				return nil, errors.New("trailing slash")
			}
		}
	}
	split = append(split, name[lastPos:])
	for _, s := range split {
		if s == "" || s == "." || s == ".." {
			return nil, errors.New("invalid level of directory: '" + s + "'")
		}
	}

	// Check mode
	modeType := mode & fs.ModeType
	if modeType != 0 && modeType != fs.ModeDir {
		return nil, errors.New("invalid mode type")
	}

	// Check existence
	if _, ok := w.fileIndex[name]; ok {
		return nil, fs.ErrExist
	}

	// Add file
	f := &wfileDesc{
		fileHeader: fileHeader{
			name:     name,
			mode:     mode,
			modified: modified,
		},
		writer: w,
	}
	if modeType == fs.ModeDir {
		f.closed = true
	} else {
		f.offset = w.getPos()
	}
	w.fileIndex[name] = len(w.file)
	w.file = append(w.file, &f.fileHeader)
	return f, nil
}

func (w *Writer) Close() error {
	w.padTo32()
	metaStart := w.getPos()
	buf := make([]byte, 32)

	// Write file metadata
	for _, h := range w.file {
		binary.LittleEndian.PutUint64(buf, uint64(h.offset))
		binary.LittleEndian.PutUint64(buf[8:], uint64(h.size))
		binary.LittleEndian.PutUint32(buf[16:], uint32(h.mode))
		binary.LittleEndian.PutUint32(buf[20:], uint32(h.modified.Nanosecond()))
		binary.LittleEndian.PutUint64(buf[24:], uint64(h.modified.Unix()))
		if _, err := w.write(buf); err != nil {
			return err
		}
	}

	// Write file names
	for _, h := range w.file {
		buf := []byte(h.name)
		buf = append(buf, 0)
		if _, err := w.write(buf); err != nil {
			return err
		}
	}

	// Write the final block
	w.padTo32()
	metaEnd := w.getPos() + 32
	binary.LittleEndian.PutUint64(buf, uint64(metaEnd-metaStart))
	binary.LittleEndian.PutUint64(buf[8:], uint64(len(w.file)))
	if _, err := rand.Read(buf[16:24]); err != nil {
		return err
	}
	binary.LittleEndian.PutUint64(buf[24:], 0)
	if _, err := w.write(buf); err != nil {
		return err
	}

	// Update header
	binary.LittleEndian.PutUint64(buf, uint64(metaEnd))
	if _, err := w.fd.WriteAt(buf[:8], 8); err != nil {
		return err
	}

	return w.fd.Close()
}

func (w *Writer) write(p []byte) (n int, err error) {
	w.buf = append(w.buf, p...)
	if len(w.buf) < 16 {
		return len(p), nil
	}

	end := len(w.buf) / 16 * 16
	buf := w.buf[:end]
	w.xorKeyStream(buf, buf, w.pos)

	n, err = w.fd.Write(buf)
	if err != nil {
		return 0, err
	}
	w.pos += int64(n)

	buf = make([]byte, len(w.buf)-end)
	copy(buf, w.buf[end:])
	w.buf = buf
	return len(p), nil
}

func (w *Writer) padTo32() {
	n := w.getPos() % 32
	if n != 0 {
		w.buf = append(w.buf, make([]byte, 32-n)...)
	}
}

func (w *Writer) getPos() int64 {
	return w.pos + int64(len(w.buf))
}

// wfileDesc represents an open file for write.
type wfileDesc struct {
	fileHeader
	writer *Writer
	closed bool
}

func (f *wfileDesc) Write(p []byte) (n int, err error) {
	if f.closed {
		return 0, fs.ErrClosed
	}
	n, err = f.writer.write(p)
	f.size += int64(n)
	return n, err
}

func (f *wfileDesc) Close() error {
	f.closed = true
	return nil
}
