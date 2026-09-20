// Package msgpack implements a dependency-free MessagePack encoder and
// decoder with streaming I/O, canonical map key ordering, and configurable
// limits against malicious payloads.
package msgpack

import (
	"errors"
	"io"
)

// Recognizable errors returned by this package. Use errors.Is to match.
var (
	// ErrReservedByte is returned when the reserved format byte 0xc1 is read.
	ErrReservedByte = errors.New("msgpack: reserved format byte 0xc1")
	// ErrTruncated is returned when input ends in the middle of a value.
	ErrTruncated = errors.New("msgpack: truncated input")
	// ErrTrailingData is returned by Decode when extra bytes follow a value.
	ErrTrailingData = errors.New("msgpack: trailing data after value")
	// ErrInvalidUTF8 is returned for strings that are not valid UTF-8.
	ErrInvalidUTF8 = errors.New("msgpack: invalid UTF-8 string")
	// ErrDepthExceeded is returned when container nesting exceeds the limit.
	ErrDepthExceeded = errors.New("msgpack: max depth exceeded")
	// ErrSizeExceeded is returned when a value exceeds the size limit.
	ErrSizeExceeded = errors.New("msgpack: value size limit exceeded")
	// ErrInvalidMapKey is returned for map keys that are not string/integer.
	ErrInvalidMapKey = errors.New("msgpack: invalid map key type")
	// ErrUnsupportedType is returned when encoding an unsupported Go type.
	ErrUnsupportedType = errors.New("msgpack: unsupported type")
)

// Ext is a MessagePack extension value. The type code and payload are
// preserved verbatim, so unknown extensions (including timestamps, type -1)
// round-trip without loss.
type Ext struct {
	Type int8
	Data []byte
}

// Decoded Go representations:
//
//	nil            -> nil
//	bool           -> bool
//	int/uint       -> int64 (uint64 only when the value overflows int64)
//	float32/64     -> float64
//	str            -> string (validated UTF-8)
//	bin            -> []byte
//	array          -> []any
//	map            -> map[any]any (keys are string, int64, or uint64)
//	ext            -> Ext

const (
	// DefaultMaxDepth is the default maximum array/map nesting depth.
	DefaultMaxDepth = 512
	// DefaultMaxValueSize is the default maximum byte size of a single
	// str/bin/ext payload, and the sanity cap on array/map element counts.
	DefaultMaxValueSize = 1 << 20
)

type config struct {
	maxDepth int
	maxSize  int64
}

func defaultConfig() config {
	return config{maxDepth: DefaultMaxDepth, maxSize: DefaultMaxValueSize}
}

// Option configures a Decoder.
type Option func(*config)

// WithMaxDepth sets the maximum array/map nesting depth.
func WithMaxDepth(n int) Option {
	return func(c *config) { c.maxDepth = n }
}

// WithMaxValueSize sets the maximum byte size of a single str/bin/ext
// payload. It also caps array/map element counts to block bomb packets.
func WithMaxValueSize(n int) Option {
	return func(c *config) { c.maxSize = int64(n) }
}

// Decode reads exactly one value from r and fails with ErrTrailingData if
// extra bytes follow it.
func Decode(r io.Reader, opts ...Option) (any, error) {
	d := NewDecoder(r, opts...)
	v, err := d.Decode()
	if err != nil {
		return nil, err
	}
	if _, err := d.Decode(); err != io.EOF {
		if err == nil {
			return nil, ErrTrailingData
		}
		return nil, err
	}
	return v, nil
}

// Encode writes v to w in canonical form (deterministic map key order).
func Encode(w io.Writer, v any) error {
	return NewEncoder(w).Encode(v)
}
