package message

import (
	"bufio"
	"io"
	"math"
	"strings"

	"github.com/emersion/go-message/textproto"
)

// An Entity is either a whole message or a one of the parts in the body of a
// multipart entity.
//
// An Entity can only be consumed once: after its body is read, it can't be
// used anymore.
type Entity struct {
	Header Header    // The entity's header.
	Body   io.Reader // The decoded entity's body.

	mediaType   string
	mediaParams map[string]string
	mediaErr    error

	// maxHeaderBytes bounds the header block of each MIME part within this
	// entity. Zero means the textproto default; it is populated from
	// ReadOptions.MaxHeaderBytes and inherited by child parts.
	maxHeaderBytes int64
}

// New makes a new message with the provided header and body. The entity's
// transfer encoding and charset are automatically decoded to UTF-8.
//
// If the message uses an unknown transfer encoding or charset, New returns an
// error that verifies IsUnknownCharset, but also returns an Entity that can
// be read.
func New(header Header, body io.Reader) (*Entity, error) {
	return newEntity(header, body, "text/plain")
}

func newEntity(header Header, body io.Reader, defaultMediaType string) (*Entity, error) {
	var err error

	mediaType, mediaParams, mediaErr := header.ContentType()
	if !header.Has("Content-Type") {
		mediaType = defaultMediaType
	}

	// QUIRK: RFC 2045 section 6.4 specifies that multipart messages can't have
	// a Content-Transfer-Encoding other than "7bit", "8bit" or "binary".
	// However some messages in the wild are non-conformant and have it set to
	// e.g. "quoted-printable". So we just ignore it for multipart.
	// See https://github.com/emersion/go-message/issues/48
	if !strings.HasPrefix(mediaType, "multipart/") {
		enc := header.Get("Content-Transfer-Encoding")
		if decoded, encErr := encodingReader(enc, body); encErr != nil {
			err = UnknownEncodingError{encErr}
		} else {
			body = decoded
		}
	}

	// RFC 2046 section 4.1.2: charset only applies to text/*
	if strings.HasPrefix(mediaType, "text/") {
		if ch, ok := mediaParams["charset"]; ok {
			if converted, charsetErr := charsetReader(ch, body); charsetErr != nil {
				err = UnknownCharsetError{charsetErr}
			} else {
				body = converted
			}
		}
	}

	return &Entity{
		Header:      header,
		Body:        body,
		mediaType:   mediaType,
		mediaParams: mediaParams,
		mediaErr:    mediaErr,
	}, err
}

// ContentType returns the effective media type and parameters used when the
// entity was created. A missing Content-Type defaults to text/plain, except
// for a direct child read from multipart/digest, where RFC 2046 section 5.1.5
// specifies message/rfc822. This default does not add a header field.
//
// Later changes to Header do not change this result or how Body is decoded.
// The returned parameters may be modified without affecting the entity.
func (e *Entity) ContentType() (string, map[string]string, error) {
	var params map[string]string
	if e.mediaParams != nil {
		params = make(map[string]string, len(e.mediaParams))
		for k, v := range e.mediaParams {
			params[k] = v
		}
	}
	return e.mediaType, params, e.mediaErr
}

// NewMultipart makes a new multipart message with the provided header and
// parts. The Content-Type header must begin with "multipart/".
//
// If the message uses an unknown transfer encoding, NewMultipart returns an
// error that verifies IsUnknownCharset, but also returns an Entity that can
// be read.
func NewMultipart(header Header, parts []*Entity) (*Entity, error) {
	r := &multipartBody{
		header: header,
		parts:  parts,
	}

	return New(header, r)
}

const defaultMaxHeaderBytes = 1 << 20 // 1 MB

// errHeaderTooBig is the same value as textproto.ErrHeaderTooBig, so callers
// can match header-size errors from both the top-level message header and
// MIME part headers with a single errors.Is target.
var errHeaderTooBig = textproto.ErrHeaderTooBig

// limitedReader is the same as io.LimitedReader, but returns a custom error.
type limitedReader struct {
	R io.Reader
	N int64
}

func (lr *limitedReader) Read(p []byte) (int, error) {
	if lr.N <= 0 {
		return 0, errHeaderTooBig
	}
	if int64(len(p)) > lr.N {
		p = p[0:lr.N]
	}
	n, err := lr.R.Read(p)
	lr.N -= int64(n)
	return n, err
}

// ReadOptions are options for ReadWithOptions.
type ReadOptions struct {
	// MaxHeaderBytes limits the maximum permissible size of a message header
	// block. It applies to the top-level message header and to the header of
	// each MIME part parsed from the returned entity. If exceeded, an error
	// matching textproto.ErrHeaderTooBig will be returned.
	//
	// Set to -1 for no limit, set to 0 for the default value (1MB).
	MaxHeaderBytes int64
}

// withDefaults returns a sanitised version of the options with defaults/special
// values accounted for.
func (o *ReadOptions) withDefaults() *ReadOptions {
	var out ReadOptions
	if o != nil {
		out = *o
	}
	if out.MaxHeaderBytes == 0 {
		out.MaxHeaderBytes = defaultMaxHeaderBytes
	} else if out.MaxHeaderBytes < 0 {
		out.MaxHeaderBytes = math.MaxInt64
	}
	return &out
}

// ReadWithOptions see Read, but allows overriding some parameters with
// ReadOptions.
//
// If the message uses an unknown transfer encoding or charset, ReadWithOptions
// returns an error that verifies IsUnknownCharset or IsUnknownEncoding, but
// also returns an Entity that can be read.
func ReadWithOptions(r io.Reader, opts *ReadOptions) (*Entity, error) {
	opts = opts.withDefaults()

	lr := &limitedReader{R: r, N: opts.MaxHeaderBytes}
	br := bufio.NewReader(lr)

	h, err := textproto.ReadHeader(br)
	if err != nil {
		return nil, err
	}

	lr.N = math.MaxInt64

	e, err := New(Header{h}, br)
	if e != nil {
		e.maxHeaderBytes = opts.MaxHeaderBytes
	}
	return e, err
}

// Read reads a message from r. The message's encoding and charset are
// automatically decoded to raw UTF-8. Note that this function only reads the
// message header.
//
// If the message uses an unknown transfer encoding or charset, Read returns an
// error that verifies IsUnknownCharset or IsUnknownEncoding, but also returns
// an Entity that can be read.
func Read(r io.Reader) (*Entity, error) {
	return ReadWithOptions(r, nil)
}

// MultipartReader returns a MultipartReader that reads parts from this entity's
// body. If this entity is not multipart, it returns nil.
func (e *Entity) MultipartReader() MultipartReader {
	if !strings.HasPrefix(e.mediaType, "multipart/") {
		return nil
	}
	if mb, ok := e.Body.(*multipartBody); ok {
		return mb
	}
	tr := textproto.NewMultipartReader(e.Body, e.mediaParams["boundary"])
	if e.maxHeaderBytes != 0 {
		tr.MaxHeaderBytes = e.maxHeaderBytes
	}
	defaultMediaType := "text/plain"
	if e.mediaType == "multipart/digest" {
		defaultMediaType = "message/rfc822"
	}
	return &multipartReader{r: tr, maxHeaderBytes: e.maxHeaderBytes, defaultMediaType: defaultMediaType}
}

// writeBodyTo writes this entity's body to w (without the header).
func (e *Entity) writeBodyTo(w *Writer) error {
	var err error
	if mb, ok := e.Body.(*multipartBody); ok {
		err = mb.writeBodyTo(w)
	} else {
		_, err = io.Copy(w, e.Body)
	}
	return err
}

// WriteTo writes this entity's header and body to w.
func (e *Entity) WriteTo(w io.Writer) error {
	ew, err := CreateWriter(w, e.Header)
	if err != nil {
		return err
	}

	if err := e.writeBodyTo(ew); err != nil {
		ew.Close()
		return err
	}

	return ew.Close()
}

// WalkFunc is the type of the function called for each part visited by Walk.
//
// The path argument is a list of multipart indices leading to the part. The
// root part has a nil path.
//
// If there was an encoding error walking to a part, the incoming error will
// describe the problem and the function can decide how to handle that error.
//
// Unlike IMAP part paths, indices start from 0 (instead of 1) and a
// non-multipart message has a nil path (instead of {1}).
//
// If an error is returned, processing stops.
type WalkFunc func(path []int, entity *Entity, err error) error

// Walk walks the entity's multipart tree, calling walkFunc for each part in
// the tree, including the root entity.
//
// Walk consumes the entity.
func (e *Entity) Walk(walkFunc WalkFunc) error {
	var multipartReaders []MultipartReader
	var path []int
	part := e
	for {
		var err error
		if part == nil {
			if len(multipartReaders) == 0 {
				break
			}

			// Get the next part from the last multipart reader
			mr := multipartReaders[len(multipartReaders)-1]
			part, err = mr.NextPart()
			if err == io.EOF {
				multipartReaders = multipartReaders[:len(multipartReaders)-1]
				path = path[:len(path)-1]
				continue
			} else if IsUnknownEncoding(err) || IsUnknownCharset(err) {
				// Forward the error to walkFunc
			} else if err != nil {
				return err
			}

			path[len(path)-1]++
		}

		// Copy the path since we'll mutate it on the next iteration
		var pathCopy []int
		if len(path) > 0 {
			pathCopy = make([]int, len(path))
			copy(pathCopy, path)
		}

		if err := walkFunc(pathCopy, part, err); err != nil {
			return err
		}

		if mr := part.MultipartReader(); mr != nil {
			multipartReaders = append(multipartReaders, mr)
			path = append(path, -1)
		}

		part = nil
	}

	return nil
}
