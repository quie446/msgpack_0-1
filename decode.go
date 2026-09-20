package msgpack

import (
	"fmt"
	"io"
	"math"
	"unicode/utf8"
)

// Decoder reads MessagePack values from an io.Reader. It never assumes the
// whole message is in memory: input may arrive one byte at a time.
type Decoder struct {
	r   io.Reader
	cfg config
	buf [8]byte
}

// NewDecoder returns a Decoder reading from r.
func NewDecoder(r io.Reader, opts ...Option) *Decoder {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}
	return &Decoder{r: r, cfg: cfg}
}

// Decode reads the next value. It returns io.EOF only when the stream ends
// cleanly at a value boundary; a stream ending mid-value yields ErrTruncated.
func (d *Decoder) Decode() (any, error) {
	b, err := d.readByte()
	if err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			return nil, err
		}
		// Clean end of stream at a value boundary.
		return nil, io.EOF
	}
	return d.decodeValue(b, 0)
}

func (d *Decoder) readByte() (byte, error) {
	_, err := io.ReadFull(d.r, d.buf[:1])
	if err != nil {
		return 0, err
	}
	return d.buf[0], nil
}

// mustByte reads a byte mid-value; EOF there means truncation.
func (d *Decoder) mustByte() (byte, error) {
	b, err := d.readByte()
	if err != nil {
		return 0, ErrTruncated
	}
	return b, nil
}

func (d *Decoder) readUint(n int) (uint64, error) {
	if _, err := io.ReadFull(d.r, d.buf[:n]); err != nil {
		return 0, ErrTruncated
	}
	var u uint64
	for i := 0; i < n; i++ {
		u = u<<8 | uint64(d.buf[i])
	}
	return u, nil
}

func (d *Decoder) readData(n int64) ([]byte, error) {
	if n < 0 || n > d.cfg.maxSize {
		return nil, fmt.Errorf("%w: %d bytes (limit %d)", ErrSizeExceeded, n, d.cfg.maxSize)
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(d.r, data); err != nil {
		return nil, ErrTruncated
	}
	return data, nil
}

func (d *Decoder) checkCount(n int64) error {
	if n > d.cfg.maxSize {
		return fmt.Errorf("%w: %d elements (limit %d)", ErrSizeExceeded, n, d.cfg.maxSize)
	}
	return nil
}

func (d *Decoder) decodeValue(b byte, depth int) (any, error) {
	switch {
	case b <= 0x7f: // positive fixint
		return int64(b), nil
	case b >= 0xe0: // negative fixint
		return int64(int8(b)), nil
	case b >= 0x80 && b <= 0x8f: // fixmap
		return d.decodeMap(int64(b&0x0f), depth)
	case b >= 0x90 && b <= 0x9f: // fixarray
		return d.decodeArray(int64(b&0x0f), depth)
	case b >= 0xa0 && b <= 0xbf: // fixstr
		return d.decodeStr(int64(b & 0x1f))
	}

	switch b {
	case 0xc0:
		return nil, nil
	case 0xc1:
		return nil, ErrReservedByte
	case 0xc2:
		return false, nil
	case 0xc3:
		return true, nil
	case 0xc4:
		n, err := d.readUint(1)
		if err != nil {
			return nil, err
		}
		return d.decodeBin(int64(n))
	case 0xc5:
		n, err := d.readUint(2)
		if err != nil {
			return nil, err
		}
		return d.decodeBin(int64(n))
	case 0xc6:
		n, err := d.readUint(4)
		if err != nil {
			return nil, err
		}
		return d.decodeBin(int64(n))
	case 0xc7:
		n, err := d.readUint(1)
		if err != nil {
			return nil, err
		}
		return d.decodeExt(int64(n))
	case 0xc8:
		n, err := d.readUint(2)
		if err != nil {
			return nil, err
		}
		return d.decodeExt(int64(n))
	case 0xc9:
		n, err := d.readUint(4)
		if err != nil {
			return nil, err
		}
		return d.decodeExt(int64(n))
	case 0xca:
		u, err := d.readUint(4)
		if err != nil {
			return nil, err
		}
		return float64(math.Float32frombits(uint32(u))), nil
	case 0xcb:
		u, err := d.readUint(8)
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(u), nil
	case 0xcc:
		u, err := d.readUint(1)
		if err != nil {
			return nil, err
		}
		return int64(u), nil
	case 0xcd:
		u, err := d.readUint(2)
		if err != nil {
			return nil, err
		}
		return int64(u), nil
	case 0xce:
		u, err := d.readUint(4)
		if err != nil {
			return nil, err
		}
		return int64(u), nil
	case 0xcf:
		u, err := d.readUint(8)
		if err != nil {
			return nil, err
		}
		if u <= math.MaxInt64 {
			return int64(u), nil
		}
		return u, nil
	case 0xd0:
		u, err := d.readUint(1)
		if err != nil {
			return nil, err
		}
		return int64(int8(u)), nil
	case 0xd1:
		u, err := d.readUint(2)
		if err != nil {
			return nil, err
		}
		return int64(int16(u)), nil
	case 0xd2:
		u, err := d.readUint(4)
		if err != nil {
			return nil, err
		}
		return int64(int32(u)), nil
	case 0xd3:
		u, err := d.readUint(8)
		if err != nil {
			return nil, err
		}
		return int64(u), nil
	case 0xd4:
		return d.decodeExt(1)
	case 0xd5:
		return d.decodeExt(2)
	case 0xd6:
		return d.decodeExt(4)
	case 0xd7:
		return d.decodeExt(8)
	case 0xd8:
		return d.decodeExt(16)
	case 0xd9:
		n, err := d.readUint(1)
		if err != nil {
			return nil, err
		}
		return d.decodeStr(int64(n))
	case 0xda:
		n, err := d.readUint(2)
		if err != nil {
			return nil, err
		}
		return d.decodeStr(int64(n))
	case 0xdb:
		n, err := d.readUint(4)
		if err != nil {
			return nil, err
		}
		return d.decodeStr(int64(n))
	case 0xdc:
		n, err := d.readUint(2)
		if err != nil {
			return nil, err
		}
		return d.decodeArray(int64(n), depth)
	case 0xdd:
		n, err := d.readUint(4)
		if err != nil {
			return nil, err
		}
		return d.decodeArray(int64(n), depth)
	case 0xde:
		n, err := d.readUint(2)
		if err != nil {
			return nil, err
		}
		return d.decodeMap(int64(n), depth)
	case 0xdf:
		n, err := d.readUint(4)
		if err != nil {
			return nil, err
		}
		return d.decodeMap(int64(n), depth)
	}
	// Unreachable: all 256 byte values are covered above.
	return nil, fmt.Errorf("msgpack: unknown format byte 0x%02x", b)
}

func (d *Decoder) decodeStr(n int64) (any, error) {
	data, err := d.readData(n)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, ErrInvalidUTF8
	}
	return string(data), nil
}

func (d *Decoder) decodeBin(n int64) (any, error) {
	data, err := d.readData(n)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (d *Decoder) decodeExt(n int64) (any, error) {
	t, err := d.mustByte()
	if err != nil {
		return nil, err
	}
	data, err := d.readData(n)
	if err != nil {
		return nil, err
	}
	return Ext{Type: int8(t), Data: data}, nil
}

func (d *Decoder) decodeArray(n int64, depth int) (any, error) {
	if depth >= d.cfg.maxDepth {
		return nil, ErrDepthExceeded
	}
	if err := d.checkCount(n); err != nil {
		return nil, err
	}
	arr := make([]any, n)
	for i := range arr {
		b, err := d.mustByte()
		if err != nil {
			return nil, err
		}
		v, err := d.decodeValue(b, depth+1)
		if err != nil {
			return nil, err
		}
		arr[i] = v
	}
	return arr, nil
}

func (d *Decoder) decodeMap(n int64, depth int) (any, error) {
	if depth >= d.cfg.maxDepth {
		return nil, ErrDepthExceeded
	}
	if err := d.checkCount(n); err != nil {
		return nil, err
	}
	m := make(map[any]any, n)
	for i := int64(0); i < n; i++ {
		kb, err := d.mustByte()
		if err != nil {
			return nil, err
		}
		k, err := d.decodeValue(kb, depth+1)
		if err != nil {
			return nil, err
		}
		switch k.(type) {
		case string, int64, uint64:
		default:
			return nil, fmt.Errorf("%w: %T", ErrInvalidMapKey, k)
		}
		vb, err := d.mustByte()
		if err != nil {
			return nil, err
		}
		v, err := d.decodeValue(vb, depth+1)
		if err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, nil
}
