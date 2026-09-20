package msgpack

import "errors"

var (
	ErrReservedFormat  = errors.New("msgpack: reserved format byte 0xc1")
	ErrInvalidUTF8     = errors.New("msgpack: string is not valid UTF-8")
	ErrInvalidMapKey   = errors.New("msgpack: map key must be string or integer")
	ErrDepthLimit      = errors.New("msgpack: nesting depth limit exceeded")
	ErrSizeLimit       = errors.New("msgpack: value size limit exceeded")
	ErrTrailingData    = errors.New("msgpack: trailing data after top-level value")
	ErrUnsupportedType = errors.New("msgpack: unsupported Go type")
)
