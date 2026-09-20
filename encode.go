package msgpack

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
	"unicode/utf8"
)

// Encoder writes MessagePack values to an io.Writer, one value per Encode
// call. Map keys are emitted sorted by their canonical encoded bytes, so
// encoding the same map twice yields identical output.
type Encoder struct {
	w   io.Writer
	buf []byte
}

func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

func (e *Encoder) Encode(v interface{}) error {
	e.buf = e.buf[:0]
	if err := encodeValue(&e.buf, v); err != nil {
		return err
	}
	for len(e.buf) > 0 {
		n, err := e.w.Write(e.buf)
		if err != nil {
			return err
		}
		e.buf = e.buf[n:]
	}
	return nil
}

func encodeValue(buf *[]byte, v interface{}) error {
	switch v := v.(type) {
	case nil:
		*buf = append(*buf, 0xc0)
	case bool:
		if v {
			*buf = append(*buf, 0xc3)
		} else {
			*buf = append(*buf, 0xc2)
		}
	case string:
		if !utf8.ValidString(v) {
			return ErrInvalidUTF8
		}
		*buf = appendString(*buf, v)
	case []byte:
		*buf = appendBin(*buf, v)
	case Ext:
		*buf = appendExt(*buf, v)
	case float32:
		*buf = append(*buf, 0xca, 0, 0, 0, 0)
		b := math.Float32bits(v)
		(*buf)[len(*buf)-4] = byte(b >> 24)
		(*buf)[len(*buf)-3] = byte(b >> 16)
		(*buf)[len(*buf)-2] = byte(b >> 8)
		(*buf)[len(*buf)-1] = byte(b)
	case float64:
		*buf = append(*buf, 0xcb, 0, 0, 0, 0, 0, 0, 0, 0)
		b := math.Float64bits(v)
		for i := 0; i < 8; i++ {
			(*buf)[len(*buf)-8+i] = byte(b >> (56 - 8*i))
		}
	case int:
		*buf = appendInt(*buf, int64(v))
	case int8:
		*buf = appendInt(*buf, int64(v))
	case int16:
		*buf = appendInt(*buf, int64(v))
	case int32:
		*buf = appendInt(*buf, int64(v))
	case int64:
		*buf = appendInt(*buf, v)
	case uint:
		*buf = appendUint(*buf, uint64(v))
	case uint8:
		*buf = appendUint(*buf, uint64(v))
	case uint16:
		*buf = appendUint(*buf, uint64(v))
	case uint32:
		*buf = appendUint(*buf, uint64(v))
	case uint64:
		*buf = appendUint(*buf, v)
	case []interface{}:
		*buf = appendArrayHeader(*buf, len(v))
		for _, e := range v {
			if err := encodeValue(buf, e); err != nil {
				return err
			}
		}
	case map[interface{}]interface{}:
		pairs := make([]mapPair, 0, len(v))
		for k, val := range v {
			kb, err := encodeMapKey(k)
			if err != nil {
				return err
			}
			pairs = append(pairs, mapPair{key: kb, val: val})
		}
		return encodePairs(buf, pairs)
	default:
		return encodeReflect(buf, reflect.ValueOf(v))
	}
	return nil
}

func encodeReflect(buf *[]byte, rv reflect.Value) error {
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		n := rv.Len()
		*buf = appendArrayHeader(*buf, n)
		for i := 0; i < n; i++ {
			if err := encodeValue(buf, rv.Index(i).Interface()); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		pairs := make([]mapPair, 0, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			kb, err := encodeMapKey(iter.Key().Interface())
			if err != nil {
				return err
			}
			pairs = append(pairs, mapPair{key: kb, val: iter.Value().Interface()})
		}
		return encodePairs(buf, pairs)
	case reflect.Ptr, reflect.Interface:
		if rv.IsNil() {
			*buf = append(*buf, 0xc0)
			return nil
		}
		return encodeValue(buf, rv.Elem().Interface())
	}
	return fmt.Errorf("%w: %s", ErrUnsupportedType, rv.Type())
}

// mapPair holds a pre-encoded key and its value.
type mapPair struct {
	key []byte
	val interface{}
}

func encodePairs(buf *[]byte, pairs []mapPair) error {
	sort.Slice(pairs, func(i, j int) bool {
		return bytes.Compare(pairs[i].key, pairs[j].key) < 0
	})
	*buf = appendMapHeader(*buf, len(pairs))
	for _, p := range pairs {
		*buf = append(*buf, p.key...)
		if err := encodeValue(buf, p.val); err != nil {
			return err
		}
	}
	return nil
}

// encodeMapKey encodes a map key, which must be a string or an integer.
func encodeMapKey(k interface{}) ([]byte, error) {
	switch k := k.(type) {
	case string:
		if !utf8.ValidString(k) {
			return nil, ErrInvalidUTF8
		}
		return appendString(nil, k), nil
	case int:
		return appendInt(nil, int64(k)), nil
	case int8:
		return appendInt(nil, int64(k)), nil
	case int16:
		return appendInt(nil, int64(k)), nil
	case int32:
		return appendInt(nil, int64(k)), nil
	case int64:
		return appendInt(nil, k), nil
	case uint:
		return appendUint(nil, uint64(k)), nil
	case uint8:
		return appendUint(nil, uint64(k)), nil
	case uint16:
		return appendUint(nil, uint64(k)), nil
	case uint32:
		return appendUint(nil, uint64(k)), nil
	case uint64:
		return appendUint(nil, k), nil
	}
	return nil, fmt.Errorf("%w: got %T", ErrInvalidMapKey, k)
}

func appendUint(buf []byte, v uint64) []byte {
	switch {
	case v <= 0x7f:
		return append(buf, byte(v))
	case v <= math.MaxUint8:
		return append(buf, 0xcc, byte(v))
	case v <= math.MaxUint16:
		return append(buf, 0xcd, byte(v>>8), byte(v))
	case v <= math.MaxUint32:
		return append(buf, 0xce, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
	default:
		return append(buf, 0xcf,
			byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32),
			byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
	}
}

func appendInt(buf []byte, v int64) []byte {
	if v >= 0 {
		return appendUint(buf, uint64(v))
	}
	switch {
	case v >= -32:
		return append(buf, byte(int8(v)))
	case v >= math.MinInt8:
		return append(buf, 0xd0, byte(int8(v)))
	case v >= math.MinInt16:
		return append(buf, 0xd1, byte(int16(v)>>8), byte(int16(v)))
	case v >= math.MinInt32:
		u := uint32(int32(v))
		return append(buf, 0xd2, byte(u>>24), byte(u>>16), byte(u>>8), byte(u))
	default:
		u := uint64(v)
		return append(buf, 0xd3,
			byte(u>>56), byte(u>>48), byte(u>>40), byte(u>>32),
			byte(u>>24), byte(u>>16), byte(u>>8), byte(u))
	}
}

func appendString(buf []byte, s string) []byte {
	n := len(s)
	switch {
	case n <= 31:
		buf = append(buf, 0xa0|byte(n))
	case n <= math.MaxUint8:
		buf = append(buf, 0xd9, byte(n))
	case n <= math.MaxUint16:
		buf = append(buf, 0xda, byte(n>>8), byte(n))
	default:
		buf = append(buf, 0xdb, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
	return append(buf, s...)
}

func appendBin(buf, data []byte) []byte {
	n := len(data)
	switch {
	case n <= math.MaxUint8:
		buf = append(buf, 0xc4, byte(n))
	case n <= math.MaxUint16:
		buf = append(buf, 0xc5, byte(n>>8), byte(n))
	default:
		buf = append(buf, 0xc6, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
	return append(buf, data...)
}

func appendExt(buf []byte, x Ext) []byte {
	n := len(x.Data)
	switch n {
	case 1:
		buf = append(buf, 0xd4)
	case 2:
		buf = append(buf, 0xd5)
	case 4:
		buf = append(buf, 0xd6)
	case 8:
		buf = append(buf, 0xd7)
	case 16:
		buf = append(buf, 0xd8)
	default:
		switch {
		case n <= math.MaxUint8:
			buf = append(buf, 0xc7, byte(n))
		case n <= math.MaxUint16:
			buf = append(buf, 0xc8, byte(n>>8), byte(n))
		default:
			buf = append(buf, 0xc9, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
		}
	}
	buf = append(buf, byte(x.Type))
	return append(buf, x.Data...)
}

func appendArrayHeader(buf []byte, n int) []byte {
	switch {
	case n <= 15:
		return append(buf, 0x90|byte(n))
	case n <= math.MaxUint16:
		return append(buf, 0xdc, byte(n>>8), byte(n))
	default:
		return append(buf, 0xdd, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
}

func appendMapHeader(buf []byte, n int) []byte {
	switch {
	case n <= 15:
		return append(buf, 0x80|byte(n))
	case n <= math.MaxUint16:
		return append(buf, 0xde, byte(n>>8), byte(n))
	default:
		return append(buf, 0xdf, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
}
