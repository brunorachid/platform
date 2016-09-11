package stdcopy

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/Sirupsen/logrus"
)

// StdType is the type of standard stream
// a writer can multiplex to.
type StdType byte

const (
	// Stdin represents standard input stream type.
	Stdin StdType = iota
	// Stdout represents standard output stream type.
	Stdout
	// Stderr represents standard error stream type.
	Stderr
	// Data represents application data stream type.
	Data

	stdWriterPrefixLen = 8
	stdWriterFdIndex   = 0
	stdWriterSizeIndex = 4

	startingBufLen = 32*1024 + stdWriterPrefixLen + 1
)

var bufPool = &sync.Pool{New: func() interface{} { return bytes.NewBuffer(nil) }}

// stdWriter is wrapper of io.Writer with extra customized info.
type stdWriter struct {
	io.Writer
	prefix byte
}

type flusher interface {
	Flush()
}

type errFlusher interface {
	Flush() error
}

// Write sends the buffer to the underneath writer.
// It inserts the prefix header before the buffer,
// so stdcopy.Copy knows where to multiplex the output.
// It makes stdWriter to implement io.Writer.
func (w *stdWriter) Write(p []byte) (n int, err error) {
	if w == nil || w.Writer == nil {
		return 0, errors.New("Writer not instantiated")
	}
	if p == nil {
		return 0, nil
	}

	header := [stdWriterPrefixLen]byte{stdWriterFdIndex: w.prefix}
	binary.BigEndian.PutUint32(header[stdWriterSizeIndex:], uint32(len(p)))
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Write(header[:])
	buf.Write(p)

	n, err = w.Writer.Write(buf.Bytes())
	n -= stdWriterPrefixLen
	if n < 0 {
		n = 0
	}

	switch b := w.Writer.(type) {
	case flusher:
		b.Flush()
	case errFlusher:
		err = b.Flush()
	}

	buf.Reset()
	bufPool.Put(buf)
	return
}

// NewWriter instantiates a new Writer.
// Everything written to it will be encapsulated using a custom format,
// and written to the underlying `w` stream.
// This allows multiple write streams (e.g. stdout and stderr) to be muxed
// into a single connection.
// `t` indicates the id of the stream to encapsulate.
// It can be stdcopy.Stdin, stdcopy.Stdout, stdcopy.Stderr.
func NewWriter(w io.Writer, t StdType) io.Writer {
	return &stdWriter{
		Writer: w,
		prefix: byte(t),
	}
}

// Copy is a modified version of io.Copy.
//
// Copy will demultiplex `src`, assuming that it contains two streams,
// previously multiplexed together using a stdWriter instance.
// As it reads from `src`, Copy will write to `dstout` and `dsterr`.
//
// Copy will read until it hits EOF on `src`. It will then return a nil error.
// In other words: if `err` is non nil, it indicates a real underlying error.
//
// `written` will hold the total number of bytes written to `dstout` and `dsterr`.
func Copy(dstout, dsterr, data io.Writer, src io.Reader) (written int64, err error) {
	var (
		buf       = make([]byte, startingBufLen)
		bufLen    = len(buf)
		nr, nw    int
		er, ew    error
		out       io.Writer
		frameSize int
	)

	for {
		// Make sure we have at least a full header
		for nr < stdWriterPrefixLen {
			var nr2 int
			nr2, er = src.Read(buf[nr:])
			nr += nr2
			if er == io.EOF {
				if nr < stdWriterPrefixLen {
					logrus.Debugf("Corrupted prefix: %v", buf[:nr])
					return written, nil
				}
				break
			}
			if er != nil {
				logrus.Debugf("Error reading header: %s", er)
				return 0, er
			}
		}

		// Check the first byte to know where to write
		switch StdType(buf[stdWriterFdIndex]) {
		case Stdin:
			fallthrough
		case Stdout:
			// Write on stdout
			out = dstout
		case Stderr:
			// Write on stderr
			out = dsterr
		case Data:
			// Write on data stream
			out = data
		default:
			logrus.Debugf("Error selecting output fd: (%d)", buf[stdWriterFdIndex])
			return 0, fmt.Errorf("Unrecognized input header: %d", buf[stdWriterFdIndex])
		}

		// Retrieve the size of the frame
		frameSize = int(binary.BigEndian.Uint32(buf[stdWriterSizeIndex : stdWriterSizeIndex+4]))
		logrus.Debugf("framesize: %d", frameSize)

		// Check if the buffer is big enough to read the frame.
		// Extend it if necessary.
		if frameSize+stdWriterPrefixLen > bufLen {
			logrus.Debugf("Extending buffer cap by %d (was %d)", frameSize+stdWriterPrefixLen-bufLen+1, len(buf))
			buf = append(buf, make([]byte, frameSize+stdWriterPrefixLen-bufLen+1)...)
			bufLen = len(buf)
		}

		// While the amount of bytes read is less than the size of the frame + header, we keep reading
		for nr < frameSize+stdWriterPrefixLen {
			var nr2 int
			nr2, er = src.Read(buf[nr:])
			nr += nr2
			if er == io.EOF {
				if nr < frameSize+stdWriterPrefixLen {
					logrus.Debugf("Corrupted frame: %v", buf[stdWriterPrefixLen:nr])
					return written, nil
				}
				break
			}
			if er != nil {
				logrus.Debugf("Error reading frame: %s", er)
				return 0, er
			}
		}

		if out != nil {
			// Write the retrieved frame (without header)
			nw, ew = out.Write(buf[stdWriterPrefixLen : frameSize+stdWriterPrefixLen])
			if ew != nil {
				logrus.Debugf("Error writting frame: %s", ew)
				return 0, ew
			}
			// If the frame has not been fully written: error
			if nw != frameSize {
				logrus.Debugf("Error short write: (%d on %d)", nw, frameSize)
				return 0, io.ErrShortWrite
			}
		} else {
			nw = frameSize
		}
		written += int64(nw)

		// Move the rest of the buffer to the beginning
		copy(buf, buf[frameSize+stdWriterPrefixLen:])
		// Move the index
		nr -= frameSize + stdWriterPrefixLen
	}
}
