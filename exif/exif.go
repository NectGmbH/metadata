// Package exif implements an JPEG/Exif decoder and encoder.
package exif

import (
	"encoding/binary"
	"errors"
	"io"

	xjpeg "github.com/tajtiattila/metadata/jpeg"
)

var (
	ErrExifNotFound = errors.New("exif: exif data not found")
	ErrExifDecode   = errors.New("exif: jpeg decode error")
	ErrExifEncode   = errors.New("exif: jpeg encode error")
)

// Exif represents Exif format metadata in JPEG/Exif files.
type Exif struct {
	// ByteOrder is the byte order used for decoding and encoding
	binary.ByteOrder

	// Main image TIFF metadata
	IFD0 Dir

	// Main image sub-IFDs
	Exif, GPS, Interop Dir

	// thumbnail
	IFD1  Dir    // Metadata
	Thumb []byte // Raw image data, typically JPEG
}

// Decode decodes Exif data from r.
func Decode(r io.Reader) (*Exif, error) {
	raw, err := exifFromReader(r)
	if err != nil {
		return nil, err
	}
	return DecodeBytes(raw)
}

var (
	jfifChunkHeader = []byte("\xff\xe0--JFIF\x00")
	jfxxChunkHeader = []byte("\xff\xe0--JFXX\x00")
	exifChunkHeader = []byte("\xff\xe1--Exif\x00\x00")
)

// Copy copies the data from r to w, replacing the
// Exif metadata in r with x. If x is nil, no
// Exif metadata is written to w. The original Exif
// metadata in r is always discarded.
// Other content such as raw image data is written
// to w unmodified.
func Copy(w io.Writer, r io.Reader, x *Exif) error {
	j, err := xjpeg.NewScanner(r)
	if err != nil {
		return err
	}

	var exifdata []byte
	if x != nil {
		var err error
		exifdata, err = x.EncodeBytes()
		if err != nil {
			return err
		}
	}

	var chunks [][]byte
	var jfifChunk, jfxxChunk []byte
	var hasExif bool
	has := 0

	for has < 3 && j.Next() {
		chunk, err := j.ReadChunk()
		if err != nil {
			return err
		}

		switch {
		case jfifChunk == nil && cmpChunkHeader(chunk, jfifChunkHeader):
			jfifChunkHeader = chunk
			has++
		case jfxxChunk == nil && cmpChunkHeader(chunk, jfxxChunkHeader):
			jfxxChunkHeader = chunk
			has++
		case !hasExif && cmpChunkHeader(chunk, exifChunkHeader):
			hasExif = true
			has++
		default:
			// unrecognised or duplicate chunk
			chunks = append(chunks, chunk)
		}
	}
	if err := j.Err(); err != nil {
		return err
	}

	// write chunks in standard jpeg/jfif header order
	ww := errw{w: w}
	ww.write(chunks[0])
	ww.write(jfifChunk)
	ww.write(jfxxChunk)

	if exifdata != nil {
		err := xjpeg.WriteChunk(w, 0xe1, exifdata)
		if err != nil {
			return err
		}
	}

	// write other chunks in jpeg (DCT, COM, APP1/XMP...)
	for _, chunk := range chunks[1:] {
		ww.write(chunk)
	}

	if ww.err != nil {
		return ww.err
	}

	// copy bytes unread so far, such as actual image data
	_, err = io.Copy(w, j.Reader())
	return err
}

type errw struct {
	w   io.Writer
	err error
}

func (w *errw) write(p []byte) {
	_, w.err = w.w.Write(p)
}

// gets raw exif as []byte
func exifFromReader(r io.Reader) ([]byte, error) {
	j, err := xjpeg.NewScanner(r)
	if err != nil {
		return nil, err
	}

	for j.Next() {
		if !j.StartChunk() || !cmpChunkHeader(j.Bytes(), exifChunkHeader) {
			continue
		}

		p, err := j.ReadChunk()
		if err != nil {
			return nil, err
		}

		// trim exif header
		return p[len(exifChunkHeader):], nil
	}

	err = j.Err()
	if err != nil {
		err = ErrExifDecode
	}
	return nil, err
}

func cmpChunkHeader(p, h []byte) bool {
	if len(p) < len(h) {
		return false
	}
	for i := range h {
		if !(i == 2 || i == 3 || p[i] == h[i]) {
			return false
		}
	}
	return true
}
