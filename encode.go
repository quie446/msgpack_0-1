package msgpack

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"sort"
	"unicode/utf8"
)

// Encoder writes MessagePack values to an io.Writer. Map keys are always
// emitted in canonical order (sorted by their encoded key bytes), so
// encoding the same value twice produces identical bytes.
type Encoder struct {
	w   io.Writer
	buf [9]byte
}

// NewEncoder returns an Encoder writing to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

// Encode writes v to the underlying writer.
//
// Supported Go types: nil, bool, string (must be valid UTF-8), []byte,
// all integer types, float32, float64, []any, map[any]any, map[string]any,
// and Ext. Map keys must be strings or integers.
func (e *Encoder) Encode(v any) error {
	switch v := v.(type) {
	case nil:
		return e.writeByte(0xc0)
	case bool:
		if v {
			return e.writeByte(0xc3)
		}
		return e.writeByte(0xc2)
	case string:
		return e.encodeStr(v)
	case []byte:
		return e.encodeBin(v)
	case int:
		return e.encodeInt(int64(v))
	case int8:
		return e.encodeInt(int64(v))
	case int16:
		return e.encodeInt(int64(v))
	case int32:
		return e.encodeInt(int64(v))
	case int64:
		return e.encodeInt(v)
	case uint:
		return e.encodeUint(uint64(v))
	case uint8:
		return e.encodeUint(uint64(v))
	case uint16:
		return e.encodeUint(uint64(v))
	case uint32:
		return e.encodeUint(uint64(v))
	case uint64:
		return e.encodeUint(v)
	case float32:
		e.buf[0] = 0xca
		u := math.Float32bits(v)
		e.buf[1] = byte(u >> 24)
		e.buf[2] = byte(u >> 16)
		e.buf[3] = byte(u >> 8)
		e.buf[4] = byte(u)
		return e.write(e.buf[:5])
	case float64:
		e.buf[0] = 0xcb
		u := math.Float64bits(v)
		for i := 0; i < 8; i++ {
			e.buf[1+i] = byte(u >> (56 - 8*i))
		}
		return e.write(e.buf[:9])
	case []any:
		return e.encodeArray(v)
	case map[any]any:
		return e.encodeMap(v)
	case map[string]any:
		m := make(map[any]any, len(v))
		for k, val := range v {
			m[k] = val
		}
		return e.encodeMap(m)
	case Ext:
		return e.encodeExt(v)
	default:
		return fmt.Errorf("%w: %T", ErrUnsupportedType, v)
	}
}

func (e *Encoder) write(p []byte) error {
	for len(p) > 0 {
		n, err := e.w.Write(p)
		if err != nil {
			return err
		}
		p = p[n:]
	}
	return nil
}

func (e *Encoder) writeByte(b byte) error {
	e.buf[0] = b
	return e.write(e.buf[:1])
}

// writeHeader emits a format byte followed by a big-endian length of n bytes.
func (e *Encoder) writeHeader(format byte, u uint64, n int) error {
	e.buf[0] = format
	for i := 0; i < n; i++ {
		e.buf[1+i] = byte(u >> (8 * (n - 1 - i)))
	}
	return e.write(e.buf[:1+n])
}

func (e *Encoder) encodeInt(i int64) error {
	if i >= 0 {
		return e.encodeUint(uint64(i))
	}
	switch {
	case i >= -32:
		return e.writeByte(byte(int8(i))) // negative fixint
	case i >= math.MinInt8:
		return e.writeHeader(0xd0, uint64(int8(i)), 1)
	case i >= math.MinInt16:
		return e.writeHeader(0xd1, uint64(int16(i)), 2)
	case i >= math.MinInt32:
		return e.writeHeader(0xd2, uint64(int32(i)), 4)
	default:
		return e.writeHeader(0xd3, uint64(i), 8)
	}
}

func (e *Encoder) encodeUint(u uint64) error {
	switch {
	case u <= 0x7f:
		return e.writeByte(byte(u)) // positive fixint
	case u <= math.MaxUint8:
		return e.writeHeader(0xcc, u, 1)
	case u <= math.MaxUint16:
		return e.writeHeader(0xcd, u, 2)
	case u <= math.MaxUint32:
		return e.writeHeader(0xce, u, 4)
	default:
		return e.writeHeader(0xcf, u, 8)
	}
}

func (e *Encoder) encodeStr(s string) error {
	if !utf8.ValidString(s) {
		return ErrInvalidUTF8
	}
	n := len(s)
	switch {
	case n <= 31:
		if err := e.writeByte(0xa0 | byte(n)); err != nil {
			return err
		}
	case n <= math.MaxUint8:
		if err := e.writeHeader(0xd9, uint64(n), 1); err != nil {
			return err
		}
	case n <= math.MaxUint16:
		if err := e.writeHeader(0xda, uint64(n), 2); err != nil {
			return err
		}
	default:
		if err := e.writeHeader(0xdb, uint64(n), 4); err != nil {
			return err
		}
	}
	return e.write([]byte(s))
}

func (e *Encoder) encodeBin(b []byte) error {
	n := len(b)
	switch {
	case n <= math.MaxUint8:
		if err := e.writeHeader(0xc4, uint64(n), 1); err != nil {
			return err
		}
	case n <= math.MaxUint16:
		if err := e.writeHeader(0xc5, uint64(n), 2); err != nil {
			return err
		}
	default:
		if err := e.writeHeader(0xc6, uint64(n), 4); err != nil {
			return err
		}
	}
	return e.write(b)
}

func (e *Encoder) encodeArray(arr []any) error {
	n := len(arr)
	switch {
	case n <= 15:
		if err := e.writeByte(0x90 | byte(n)); err != nil {
			return err
		}
	case n <= math.MaxUint16:
		if err := e.writeHeader(0xdc, uint64(n), 2); err != nil {
			return err
		}
	default:
		if err := e.writeHeader(0xdd, uint64(n), 4); err != nil {
			return err
		}
	}
	for _, v := range arr {
		if err := e.Encode(v); err != nil {
			return err
		}
	}
	return nil
}

// canonicalKeyBytes validates a map key and returns its canonical encoding.
func canonicalKeyBytes(k any) ([]byte, error) {
	switch k.(type) {
	case string, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
	default:
		return nil, fmt.Errorf("%w: %T", ErrInvalidMapKey, k)
	}
	var buf bytes.Buffer
	if err := NewEncoder(&buf).Encode(k); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type mapEntry struct {
	keyBytes []byte
	val      any
}

func (e *Encoder) encodeMap(m map[any]any) error {
	entries := make([]mapEntry, 0, len(m))
	for k, v := range m {
		kb, err := canonicalKeyBytes(k)
		if err != nil {
			return err
		}
		entries = append(entries, mapEntry{keyBytes: kb, val: v})
	}
	// Canonical order: sort by the encoded key bytes.
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].keyBytes, entries[j].keyBytes) < 0
	})

	n := len(entries)
	switch {
	case n <= 15:
		if err := e.writeByte(0x80 | byte(n)); err != nil {
			return err
		}
	case n <= math.MaxUint16:
		if err := e.writeHeader(0xde, uint64(n), 2); err != nil {
			return err
		}
	default:
		if err := e.writeHeader(0xdf, uint64(n), 4); err != nil {
			return err
		}
	}
	for _, ent := range entries {
		if err := e.write(ent.keyBytes); err != nil {
			return err
		}
		if err := e.Encode(ent.val); err != nil {
			return err
		}
	}
	return nil
}

func (e *Encoder) encodeExt(x Ext) error {
	n := len(x.Data)
	switch n {
	case 1:
		if err := e.writeByte(0xd4); err != nil {
			return err
		}
	case 2:
		if err := e.writeByte(0xd5); err != nil {
			return err
		}
	case 4:
		if err := e.writeByte(0xd6); err != nil {
			return err
		}
	case 8:
		if err := e.writeByte(0xd7); err != nil {
			return err
		}
	case 16:
		if err := e.writeByte(0xd8); err != nil {
			return err
		}
	default:
		switch {
		case n <= math.MaxUint8:
			if err := e.writeHeader(0xc7, uint64(n), 1); err != nil {
				return err
			}
		case n <= math.MaxUint16:
			if err := e.writeHeader(0xc8, uint64(n), 2); err != nil {
				return err
			}
		default:
			if err := e.writeHeader(0xc9, uint64(n), 4); err != nil {
				return err
			}
		}
	}
	if err := e.writeByte(byte(x.Type)); err != nil {
		return err
	}
	return e.write(x.Data)
}
