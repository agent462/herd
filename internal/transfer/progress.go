package transfer

import "io"

// ProgressFunc is called during file transfer with the host name, bytes
// transferred so far, and total expected bytes (0 if unknown).
type ProgressFunc func(host string, transferred, total int64)

// progressWriter wraps an io.Writer and reports bytes written via a callback.
type progressWriter struct {
	w           io.Writer
	host        string
	transferred int64
	total       int64
	onProgress  ProgressFunc
}

func newProgressWriter(w io.Writer, host string, total int64, fn ProgressFunc) *progressWriter {
	return &progressWriter{
		w:          w,
		host:       host,
		total:      total,
		onProgress: fn,
	}
}

// NewProgressWriterForTest creates a progressWriter for testing purposes.
func NewProgressWriterForTest(w io.Writer, host string, total int64, fn ProgressFunc) *progressWriter {
	return newProgressWriter(w, host, total, fn)
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.w.Write(p)
	pw.transferred += int64(n)
	if pw.onProgress != nil {
		pw.onProgress(pw.host, pw.transferred, pw.total)
	}
	return n, err
}
