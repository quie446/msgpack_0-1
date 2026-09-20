package msgpack

import (
	"fmt"
	"io"
	"math"
	"unicode/utf8"
)

const (
	defaultMaxDepth     = 512
	defaultMaxValueSize = 16 << 20 // 16 MiB
)

// Options tunes decoder safety limits. Zero fields fall back to defaults.
type Options struct {
	// MaxDepth is the maximum nesting depth of arrays/maps. Default 512.
	MaxDepth int
	// MaxValueSize is the maximum byte length of a single str/bin/ext
	// payload. Default 16 MiB.
	MaxValueSize int
}

func (o *Options) withDefaults() Options {
	d := Options{MaxDepth: defaultMaxDepth, MaxValueSize: defaultMaxValueSize}
	if o != nil {
		if o.MaxDepth > 0 {
			d.MaxDepth = o.MaxDepth
		}
		if o.MaxValueSize > 0 {
			d.MaxValueSize = o.MaxValueSize
		}
	}
	return d
}

// Decoder reads MessagePack values from an io.Reader. It never assumes the
// whole stream is buffered: the reader may deliver as little as one byte
// per Read call.
type Decoder struct {
	r       io.Reader
	opts    Options
	scratch [8]byte
}

func NewDecoder(r io.Reader, opts *Options) *Decoder {
	return &Decoder{r: r, opts: opts.withDefaults()}
}

// Decode reads exactly one value. At a clean stream boundary it returns
// io.EOF; a value cut short mid-way yields io.ErrUnexpectedEOF.
func (d *Decoder) Decode() (interface{}, error) {
	return d.decodeValue(0)
}

func (d *Decoder) readByte() (byte, error) {
	_, err := io.ReadFull(d.r, d.scratch[:1])
	return d.scratch[0], err
}

func (d *Decoder) readUint(n int) (uint64, error) {
	if _, err := io.ReadFull(d.r, d.scratch[:n]); err != nil {
		return 0, err
	}
	var v uint64
	for i := 0; i < n; i++ {
		v = v<<8 | uint64(d.scratch[i])
	}
	return v, nil
}

func (d *Decoder) readBytes(n int) ([]byte, error) {
	if n > d.opts.MaxValueSize {
		return nil, fmt.Errorf("%w: %d bytes > limit %d", ErrSizeLimit, n, d.opts.MaxValueSize)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(d.r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func (d *Decoder) decodeValue(depth int) (interface{}, error) {
	if depth > d.opts.MaxDepth {
		return nil, fmt.Errorf("%w: max %d", ErrDepthLimit, d.opts.MaxDepth)
	}
	b, err := d.readByte()
	if err != nil {
		return nil, err
	}
	switch {
	case b <= 0x7f: // positive fixint
		return int64(b), nil
	case b >= 0xe0: // negative fixint
		return int64(int8(b)), nil
	case b >= 0x80 && b <= 0x8f: // fixmap
		return d.decodeMap(int(b&0x0f), depth)
	case b >= 0x90 && b <= 0x9f: // fixarray
		return d.decodeArray(int(b&0x0f), depth)
	case b >= 0xa0 && b <= 0xbf: // fixstr
		return d.decodeString(int(b & 0x1f))
	}
	switch b {
	case 0xc0:
		return nil, nil
	case 0xc1:
		return nil, ErrReservedFormat
	case 0xc2:
		return false, nil
	case 0xc3:
		return true, nil
	case 0xc4, 0xc5, 0xc6: // bin 8/16/32
		n, err := d.readUint(1 << (b - 0xc4))
		if err != nil {
			return nil, err
		}
		return d.decodeBin(int(n))
	case 0xc7, 0xc8, 0xc9: // ext 8/16/32
		n, err := d.readUint(1 << (b - 0xc7))
		if err != nil {
			return nil, err
		}
		return d.decodeExt(int(n))
	case 0xca: // float32, widened to float64
		n, err := d.readUint(4)
		if err != nil {
			return nil, err
		}
		return float64(math.Float32frombits(uint32(n))), nil
	case 0xcb: // float64
		n, err := d.readUint(8)
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(n), nil
	case 0xcc, 0xcd, 0xce, 0xcf: // uint 8/16/32/64
		n, err := d.readUint(1 << (b - 0xcc))
		if err != nil {
			return nil, err
		}
		if n <= uint64(maxInt64) {
			return int64(n), nil
		}
		return n, nil
	case 0xd0, 0xd1, 0xd2, 0xd3: // int 8/16/32/64
		n, err := d.readUint(1 << (b - 0xd0))
		if err != nil {
			return nil, err
		}
		bits := uint(8 << (b - 0xd0))
		return int64(n<<(64-bits)) >> (64 - bits), nil // sign-extend
	case 0xd4, 0xd5, 0xd6, 0xd7, 0xd8: // fixext 1/2/4/8/16
		return d.decodeExt(1 << (b - 0xd4))
	case 0xd9, 0xda, 0xdb: // str 8/16/32
		n, err := d.readUint(1 << (b - 0xd9))
		if err != nil {
			return nil, err
		}
		return d.decodeString(int(n))
	case 0xdc, 0xdd: // array 16/32
		n, err := d.readUint(2 << (b - 0xdc))
		if err != nil {
			return nil, err
		}
		return d.decodeArray(int(n), depth)
	case 0xde, 0xdf: // map 16/32
		n, err := d.readUint(2 << (b - 0xde))
		if err != nil {
			return nil, err
		}
		return d.decodeMap(int(n), depth)
	}
	return nil, fmt.Errorf("msgpack: unknown format byte 0x%02x", b)
}

func (d *Decoder) decodeString(n int) (interface{}, error) {
	buf, err := d.readBytes(n)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(buf) {
		return nil, ErrInvalidUTF8
	}
	return string(buf), nil
}

func (d *Decoder) decodeBin(n int) (interface{}, error) {
	return d.readBytes(n)
}

func (d *Decoder) decodeExt(n int) (interface{}, error) {
	t, err := d.readByte()
	if err != nil {
		return nil, err
	}
	data, err := d.readBytes(n)
	if err != nil {
		return nil, err
	}
	return Ext{Type: int8(t), Data: data}, nil
}

func (d *Decoder) decodeArray(n, depth int) (interface{}, error) {
	hint := n
	if hint > 64 {
		hint = 64 // don't pre-allocate on a hostile count
	}
	arr := make([]interface{}, 0, hint)
	for i := 0; i < n; i++ {
		v, err := d.decodeValue(depth + 1)
		if err != nil {
			return nil, eofInContainer(err)
		}
		arr = append(arr, v)
	}
	return arr, nil
}

func (d *Decoder) decodeMap(n, depth int) (interface{}, error) {
	hint := n
	if hint > 64 {
		hint = 64
	}
	m := make(map[interface{}]interface{}, hint)
	for i := 0; i < n; i++ {
		k, err := d.decodeValue(depth + 1)
		if err != nil {
			return nil, eofInContainer(err)
		}
		switch k.(type) {
		case string, int64, uint64:
		default:
			return nil, fmt.Errorf("%w: got %T", ErrInvalidMapKey, k)
		}
		v, err := d.decodeValue(depth + 1)
		if err != nil {
			return nil, eofInContainer(err)
		}
		m[k] = v
	}
	return m, nil
}

// eofInContainer turns a clean io.EOF into io.ErrUnexpectedEOF: inside a
// container whose declared count is not yet satisfied, EOF means the stream
// was cut short.
func eofInContainer(err error) error {
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}

const maxInt64 = int64(^uint64(0) >> 1)
