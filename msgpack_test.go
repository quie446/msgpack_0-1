package msgpack

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("bad hex fixture %q: %v", s, err)
	}
	return b
}

// roundTripFixtures decode to want and re-encode to the identical hex.
var roundTripFixtures = []struct {
	name string
	hex  string
	want any
}{
	{"pos fixint 0", "00", int64(0)},
	{"pos fixint 127", "7f", int64(127)},
	{"neg fixint -32", "e0", int64(-32)},
	{"neg fixint -1", "ff", int64(-1)},
	{"uint8", "cc ff", int64(255)},
	{"uint16", "cd 01 00", int64(256)},
	{"uint32", "ce 00 01 00 00", int64(65536)},
	{"uint64 small", "cf 00 00 00 01 00 00 00 00", int64(1 << 32)},
	{"uint64 max", "cf ff ff ff ff ff ff ff ff", uint64(math.MaxUint64)},
	{"int8", "d0 80", int64(-128)},
	{"int8 -96", "d0 a0", int64(-96)},
	{"int16", "d1 ff 00", int64(-256)},
	{"int32", "d2 ff ff 00 00", int64(-65536)},
	{"int64 min", "d3 80 00 00 00 00 00 00 00", int64(math.MinInt64)},
	{"nil", "c0", nil},
	{"false", "c2", false},
	{"true", "c3", true},
	{"float64 pi", "cb 40 09 21 fb 54 44 2d 18", math.Pi},
	{"float64 -0", "cb 80 00 00 00 00 00 00 00", math.Copysign(0, -1)},
	{"empty str", "a0", ""},
	{"fixstr hello", "a5 68 65 6c 6c 6f", "hello"},
	{"str8", "d9 21 " + strings.Repeat("61", 33), strings.Repeat("a", 33)},
	{"utf8 str", "a6 e4 bd a0 e5 a5 bd", "你好"},
	{"empty bin", "c4 00", []byte{}},
	{"bin8", "c4 03 01 02 03", []byte{1, 2, 3}},
	{"empty array", "90", []any{}},
	{"fixarray", "93 01 02 03", []any{int64(1), int64(2), int64(3)}},
	{"empty map", "80", map[any]any{}},
	{
		"nested map/array/bin",
		"82 a1 61 91 01 a1 62 82 a1 63 c3 a1 64 c4 02 de ad",
		map[any]any{
			"a": []any{int64(1)},
			"b": map[any]any{"c": true, "d": []byte{0xde, 0xad}},
		},
	},
	{"fixext1", "d4 01 ff", Ext{Type: 1, Data: []byte{0xff}}},
	{"timestamp ext", "d6 ff 45 d6 50 41", Ext{Type: -1, Data: []byte{0x45, 0xd6, 0x50, 0x41}}},
	{"ext8", "c7 03 07 aa bb cc", Ext{Type: 7, Data: []byte{0xaa, 0xbb, 0xcc}}},
	{"uint64 map key", "81 cf ff ff ff ff ff ff ff ff 01", map[any]any{uint64(math.MaxUint64): int64(1)}},
}

func TestRoundTripFixtures(t *testing.T) {
	for _, tc := range roundTripFixtures {
		t.Run(tc.name, func(t *testing.T) {
			raw := mustHex(t, tc.hex)
			got, err := Decode(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("Decode(%s) error: %v", tc.hex, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Decode(%s) = %#v, want %#v", tc.hex, got, tc.want)
			}
			var buf bytes.Buffer
			if err := Encode(&buf, got); err != nil {
				t.Fatalf("Encode(%#v) error: %v", got, err)
			}
			if !bytes.Equal(buf.Bytes(), raw) {
				t.Fatalf("re-encode = %x, want %s", buf.Bytes(), tc.hex)
			}
		})
	}
}

func TestDecodeFloat32(t *testing.T) {
	got, err := Decode(bytes.NewReader(mustHex(t, "ca 3f 80 00 00")))
	if err != nil {
		t.Fatal(err)
	}
	if got != float64(1) {
		t.Fatalf("got %#v, want float64(1)", got)
	}
}

func TestErrorFixtures(t *testing.T) {
	cases := []struct {
		name string
		hex  string
		want error
	}{
		{"reserved 0xc1", "c1", ErrReservedByte},
		{"invalid utf8", "a1 ff", ErrInvalidUTF8},
		{"invalid utf8 str8", "d9 02 c3 28", ErrInvalidUTF8},
		{"truncated bin payload", "c4 05 01 02", ErrTruncated},
		{"truncated str16 length", "da 00", ErrTruncated},
		{"truncated str8 no length", "d9", ErrTruncated},
		{"truncated map body", "82 a1 61 01 a1 62", ErrTruncated},
		{"truncated ext type", "d4", ErrTruncated},
		{"bool map key", "81 c2 01", ErrInvalidMapKey},
		{"array map key", "81 90 01", ErrInvalidMapKey},
		{"float map key", "81 cb 3f f0 00 00 00 00 00 00 01", ErrInvalidMapKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode(bytes.NewReader(mustHex(t, tc.hex)))
			if !errors.Is(err, tc.want) {
				t.Fatalf("Decode(%s) error = %v, want %v", tc.hex, err, tc.want)
			}
		})
	}
}

func TestTrailingData(t *testing.T) {
	_, err := Decode(bytes.NewReader(mustHex(t, "01 02")))
	if !errors.Is(err, ErrTrailingData) {
		t.Fatalf("error = %v, want ErrTrailingData", err)
	}
}

func TestDepthLimit(t *testing.T) {
	deep := strings.Repeat("91", 600) + "c0"
	_, err := Decode(bytes.NewReader(mustHex(t, deep)))
	if !errors.Is(err, ErrDepthExceeded) {
		t.Fatalf("default depth: error = %v, want ErrDepthExceeded", err)
	}

	_, err = Decode(bytes.NewReader(mustHex(t, "91 91 91 91 91 c0")), WithMaxDepth(4))
	if !errors.Is(err, ErrDepthExceeded) {
		t.Fatalf("custom depth: error = %v, want ErrDepthExceeded", err)
	}

	v, err := Decode(bytes.NewReader(mustHex(t, "91 91 91 91 c0")), WithMaxDepth(4))
	if err != nil {
		t.Fatalf("depth 4 within limit: %v", err)
	}
	if !reflect.DeepEqual(v, []any{[]any{[]any{[]any{nil}}}}) {
		t.Fatalf("got %#v", v)
	}
}

func TestSizeLimit(t *testing.T) {
	_, err := Decode(bytes.NewReader(mustHex(t, "a5 68 65 6c 6c 6f")), WithMaxValueSize(4))
	if !errors.Is(err, ErrSizeExceeded) {
		t.Fatalf("str over limit: error = %v, want ErrSizeExceeded", err)
	}

	_, err = Decode(bytes.NewReader(mustHex(t, "c4 10 "+strings.Repeat("00", 16))), WithMaxValueSize(4))
	if !errors.Is(err, ErrSizeExceeded) {
		t.Fatalf("bin over limit: error = %v, want ErrSizeExceeded", err)
	}

	// array32 declaring 1M elements with a small limit: rejected before alloc.
	_, err = Decode(bytes.NewReader(mustHex(t, "dd 00 10 00 00")), WithMaxValueSize(100))
	if !errors.Is(err, ErrSizeExceeded) {
		t.Fatalf("array count over limit: error = %v, want ErrSizeExceeded", err)
	}
}

// dripReader yields one byte per Read to prove no whole-buffer assumption.
type dripReader struct{ data []byte }

func (r *dripReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	p[0] = r.data[0]
	r.data = r.data[1:]
	return 1, nil
}

func TestStreamingOneByteAtATime(t *testing.T) {
	raw := mustHex(t, "82 a1 61 91 01 a1 62 82 a1 63 c3 a1 64 c4 02 de ad")
	want := map[any]any{
		"a": []any{int64(1)},
		"b": map[any]any{"c": true, "d": []byte{0xde, 0xad}},
	}
	got, err := Decode(&dripReader{data: raw})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestStreamMultipleValues(t *testing.T) {
	d := NewDecoder(bytes.NewReader(mustHex(t, "01 a1 78 02")))
	want := []any{int64(1), "x", int64(2)}
	for i, w := range want {
		got, err := d.Decode()
		if err != nil {
			t.Fatalf("value %d: %v", i, err)
		}
		if !reflect.DeepEqual(got, w) {
			t.Fatalf("value %d = %#v, want %#v", i, got, w)
		}
	}
	if _, err := d.Decode(); err != io.EOF {
		t.Fatalf("final Decode error = %v, want io.EOF", err)
	}
}

func TestCanonicalMixedKeyOrder(t *testing.T) {
	m := map[any]any{"b": 1, 10: 2, "a": 3}
	var buf bytes.Buffer
	if err := Encode(&buf, m); err != nil {
		t.Fatal(err)
	}
	// Keys sorted by encoded bytes: 0x0a (10) < a1 61 ("a") < a1 62 ("b").
	want := mustHex(t, "83 0a 02 a1 61 03 a1 62 01")
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("got %x, want %x", buf.Bytes(), want)
	}
}

func TestEncodeDeterministic(t *testing.T) {
	m := map[any]any{
		"sensor": "temp", "values": []any{1, -2, 3.5},
		7: "lucky", uint64(300): []byte{0x01}, -5: true,
	}
	first := mustEncode(t, m)
	for i := 0; i < 20; i++ {
		if got := mustEncode(t, m); !bytes.Equal(got, first) {
			t.Fatalf("iteration %d: %x != %x", i, got, first)
		}
	}
}

func mustEncode(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := Encode(&buf, v); err != nil {
		t.Fatalf("Encode(%#v): %v", v, err)
	}
	return buf.Bytes()
}

func TestEncodeErrors(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want error
	}{
		{"invalid utf8 string", "\xff\xfe", ErrInvalidUTF8},
		{"invalid utf8 map key", map[any]any{"\xff": 1}, ErrInvalidUTF8},
		{"bool map key", map[any]any{true: 1}, ErrInvalidMapKey},
		{"float map key", map[any]any{1.5: 1}, ErrInvalidMapKey},
		{"nil map key", map[any]any{nil: 1}, ErrInvalidMapKey},
		{"unsupported type", struct{}{}, ErrUnsupportedType},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Encode(&buf, tc.v); !errors.Is(err, tc.want) {
				t.Fatalf("Encode(%#v) error = %v, want %v", tc.v, err, tc.want)
			}
		})
	}
}

func TestEncodeStringMap(t *testing.T) {
	got := mustEncode(t, map[string]any{"x": 1})
	want := mustHex(t, "81 a1 78 01")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %x, want %x", got, want)
	}
}

func TestExtRoundTripPreserve(t *testing.T) {
	// Unknown ext type with non-fixext length survives decode -> encode intact.
	raw := mustHex(t, "c7 05 2a de ad be ef 00")
	v, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	ext, ok := v.(Ext)
	if !ok {
		t.Fatalf("got %T, want Ext", v)
	}
	if ext.Type != 42 || !bytes.Equal(ext.Data, []byte{0xde, 0xad, 0xbe, 0xef, 0x00}) {
		t.Fatalf("got %+v", ext)
	}
	if got := mustEncode(t, v); !bytes.Equal(got, raw) {
		t.Fatalf("re-encode = %x, want %x", got, raw)
	}
}
