// Package xopen makes it easy to get buffered readers and writers.
// Ropen opens a (possibly gzipped) file/process/http site for buffered reading.
// Wopen opens a (possibly gzipped) file for buffered writing.
// Both will use gzip when appropriate and will user buffered IO.
package xopen

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
	gzip "github.com/klauspost/pgzip"
	"github.com/ulikunitz/xz"
)

// ErrNoContent means nothing in the stream/file.
var ErrNoContent = errors.New("xopen: no content")

// ErrDirNotSupported means the path is a directory.
var ErrDirNotSupported = errors.New("xopen: input is a directory")

// IsGzip returns true buffered Reader has the gzip magic.
func IsGzip(b *bufio.Reader) (bool, error) {
	return CheckBytes(b, []byte{0x1f, 0x8b})
}

// IsXz returns true buffered Reader has the xz magic.
func IsXz(b *bufio.Reader) (bool, error) {
	return CheckBytes(b, []byte{0xfd, 0x37, 0x7a, 0x58, 0x5a, 0x00})
}

// IsZst returns true buffered Reader has the zstd magic.
func IsZst(b *bufio.Reader) (bool, error) {
	return CheckBytes(b, []byte{0x28, 0xB5, 0x2f, 0xfd})
}

// IsStdin checks if we are getting data from stdin.
func IsStdin() bool {
	// http://stackoverflow.com/a/26567513
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) == 0
}

// ExpandUser expands ~/path and ~otheruser/path appropriately
func ExpandUser(path string) (string, error) {
	if len(path) == 0 || path[0] != '~' {
		return path, nil
	}
	var u *user.User
	var err error
	if len(path) == 1 || path[1] == '/' {
		u, err = user.Current()
	} else {
		name := strings.Split(path[1:], "/")[0]
		u, err = user.Lookup(name)
	}
	if err != nil {
		return "", err
	}
	home := u.HomeDir
	path = home + "/" + path[1:]
	return path, nil
}

// Exists checks if a local file exits
func Exists(path string) bool {
	path, perr := ExpandUser(path)
	if perr != nil {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// CheckBytes peeks at a buffered stream and checks if the first read bytes match.
func CheckBytes(b *bufio.Reader, buf []byte) (bool, error) {

	m, err := b.Peek(len(buf))
	if err != nil {
		return false, ErrNoContent
	}
	for i := range buf {
		if m[i] != buf[i] {
			return false, nil
		}
	}
	return true, nil
}

// Reader is returned by Ropen
type Reader struct {
	*bufio.Reader
	rdr io.Reader
	gz  io.ReadCloser
}

// Close the associated files.
func (r *Reader) Close() error {
	if r.gz != nil {
		r.gz.Close()
	}
	if c, ok := r.rdr.(io.ReadCloser); ok {
		c.Close()
	}
	return nil
}

// Writer is returned by Wopen
type Writer struct {
	*bufio.Writer
	wtr *os.File
	gz  *gzip.Writer
	xw  *xz.Writer
	zw  *zstd.Encoder
}

// Close the associated files.
func (w *Writer) Close() error {
	w.Flush()
	if w.gz != nil {
		w.gz.Close()
	}
	if w.xw != nil {
		w.xw.Close()
	}
	if w.zw != nil {
		w.zw.Close()
	}
	w.wtr.Close()
	return nil
}

// Flush the writer.
func (w *Writer) Flush() {
	w.Writer.Flush()
	if w.gz != nil {
		w.gz.Flush()
	}
	if w.zw != nil {
		w.zw.Flush()
	}
}

var bufSize = 65536

// Buf returns a buffered reader from an io.Reader
// If f == "-", then it will attempt to read from os.Stdin.
// If the file is gzipped, it will be read as such.
func Buf(r io.Reader) (*Reader, error) {
	b := bufio.NewReaderSize(r, bufSize)
	var rd io.Reader
	var rdr io.ReadCloser
	if is, err := IsGzip(b); err != nil && err != io.EOF {
		return nil, err
	} else if is {
		rdr, err = gzip.NewReader(b)
		if err != nil {
			return nil, err
		}
		b = bufio.NewReaderSize(rdr, bufSize)
	} else if is, err := IsXz(b); err != nil && err != io.EOF {
		return nil, err
	} else if is {
		rd, err = xz.NewReader(b)
		if err != nil {
			return nil, err
		}
		b = bufio.NewReaderSize(rd, bufSize)
	} else if is, err := IsZst(b); err != nil && err != io.EOF {
		return nil, err
	} else if is {
		rd, err = zstd.NewReader(b)
		if err != nil {
			return nil, err
		}
		b = bufio.NewReaderSize(rd, bufSize)
	}

	// check BOM
	t, _, err := b.ReadRune()
	if err != nil {
		return nil, ErrNoContent
	}
	if t != '\uFEFF' {
		b.UnreadRune()
	}
	return &Reader{b, r, rdr}, nil
}

// XReader returns a reader from a url string or a file.
func XReader(f string) (io.Reader, error) {
	if strings.HasPrefix(f, "http://") || strings.HasPrefix(f, "https://") {
		var rsp *http.Response
		rsp, err := http.Get(f)
		if err != nil {
			return nil, err
		}
		if rsp.StatusCode != 200 {
			return nil, fmt.Errorf("http error downloading %s. status: %s", f, rsp.Status)
		}
		rdr := rsp.Body
		return rdr, nil
	}
	f, err := ExpandUser(f)
	if err != nil {
		return nil, err
	}

	fi, err := os.Stat(f)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return nil, ErrDirNotSupported
	}

	return os.Open(f)
}

// Ropen opens a buffered reader.
func Ropen(f string) (*Reader, error) {
	var err error
	var rdr io.Reader
	if f == "-" {
		if !IsStdin() {
			return nil, errors.New("stdin not detected")
		}
		b, err := Buf(os.Stdin)
		return b, err
	} else if f[0] == '|' {
		// TODO: use csv to handle quoted file names.
		cmdStrs := strings.Split(f[1:], " ")
		var cmd *exec.Cmd
		if len(cmdStrs) == 2 {
			cmd = exec.Command(cmdStrs[0], cmdStrs[1:]...)
		} else {
			cmd = exec.Command(cmdStrs[0])
		}
		rdr, err = cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		err = cmd.Start()
		if err != nil {
			return nil, err
		}
	} else {
		rdr, err = XReader(f)
	}
	if err != nil {
		return nil, err
	}
	b, err := Buf(rdr)
	return b, err
}

// Wopen opens a buffered reader.
// If f == "-", then stdout will be used.
// If f endswith ".gz", then the output will be gzipped.
// If f endswith ".xz", then the output will be zx-compressed.
// If f endswith ".zst", then the output will be zstd-compressed.
func Wopen(f string) (*Writer, error) {
	return WopenFile(f, os.O_RDONLY, 0)
}

// WopenFile opens a buffered reader.
// If f == "-", then stdout will be used.
// If f endswith ".gz", then the output will be gzipped.
// If f endswith ".xz", then the output will be zx-compressed.
// If f endswith ".zst", then the output will be zstd-compressed.
func WopenFile(f string, flag int, perm os.FileMode) (*Writer, error) {
	var wtr *os.File
	if f == "-" {
		wtr = os.Stdout
	} else {
		dir := filepath.Dir(f)
		fi, err := os.Stat(dir)
		if err == nil && !fi.IsDir() {
			return nil, fmt.Errorf("can not write file into a non-directory path: %s", dir)
		}
		if os.IsNotExist(err) {
			os.MkdirAll(dir, 0755)
		}
		wtr, err = os.OpenFile(f, flag, perm)
		if err != nil {
			return nil, err
		}
	}

	f2 := strings.ToLower(f)
	if strings.HasSuffix(f2, ".gz") {
		gz := gzip.NewWriter(wtr)
		return &Writer{bufio.NewWriterSize(gz, bufSize), wtr, gz, nil, nil}, nil
	}
	if strings.HasSuffix(f2, ".xz") {
		xw, err := xz.NewWriter(wtr)
		return &Writer{bufio.NewWriterSize(xw, bufSize), wtr, nil, xw, nil}, err
	}
	if strings.HasSuffix(f2, ".zst") {
		zw, err := zstd.NewWriter(wtr)
		return &Writer{bufio.NewWriterSize(zw, bufSize), wtr, nil, nil, zw}, err
	}
	return &Writer{bufio.NewWriterSize(wtr, bufSize), wtr, nil, nil, nil}, nil
}
