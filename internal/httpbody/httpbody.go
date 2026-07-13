package httpbody

import "io"

type limitedReadCloser struct {
	io.Reader
	io.Closer
}

func Limit(body io.ReadCloser, bytes int64) io.ReadCloser {
	return &limitedReadCloser{
		Reader: io.LimitReader(body, bytes),
		Closer: body,
	}
}
