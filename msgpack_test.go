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
		t.Fatalf("bad fixture hex %q: %v", s, err)
	}
	return b
}

// oneByteReader delivers at most one byte per Read, to prove the decoder
// does not assume the whole stream is buffered.
type oneByteReader struct {
	data []byte
	pos  int
}

func (r *oneByteReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	p[0] = r.data[r.pos]
	r.pos++
	return 1, nil
}

var roundTripFixtures = []struct {
	name string
	hex  string
	want interface{}
}{
	{"nil", "c0", nil},
	{"false", "c2", false},
	{"true", "c3", true},
	{"zero", "00", int64(0)},
	{"pos fixint max", "7f", int64(127)},
	{"neg fixint -1", "ff", int64(-1)},
	{"neg fixint -32", "e0", int64(-32)},
	{"uint8", "cc c8", int64(200)},
	{"uint16", "cd 01 00", int64(256)},
	{"uint32", "ce 00 01 00 00", int64(65536)},
	{"uint64", "cf 00 00 00 01 00 00 00 00", int64(1 << 32)},
	{"uint64 max", "cf ff ff ff ff ff ff ff ff", uint64(math.MaxUint64)},
	{"int8", "d0 9c", int64(-100)},
	{"int16", "d1 ff 00", int64(-256)},
	{"int32", "d2 ff ff 00 00", int64(-65536)},
	{"int64 min", "d3 80 00 00 00 00 00 00 00", int64(math.MinInt64)},
	{"float64 pi", "cb 40 09 21 fb 54 44 2d 18", math.Pi},
	{"fixstr", "a5 68 65 6c 6c 6f", "hello"},
	{"empty str", "a0", ""},
	{"utf8 str", "a6 e4 bd a0 e5 a5 bd", "你好"},
	{"bin8", "c4 03 01 02 03", []byte{1, 2, 3}},
	{"empty bin", "c4 00", []byte{}},
	{"fixarray", "93 01 02 03", []interface{}{int64(1), int64(2), int64(3)}},
	{"empty array", "90", []interface{}{}},
	{"empty map", "80", map[interface{}]interface{}{}},
	{"fixmap", "81 a1 61 01", map[interface{}]interface{}{"a": int64(1)}},
	{"mixed int/str keys", "82 01 02 a1 61 03",
		map[interface{}]interface{}{int64(1): int64(2), "a": int64(3)}},
	{"nested map/array/bin",
		"82 a1 61 92 01 02 a1 62 c4 03 01 02 03",
		map[interface{}]interface{}{
			"a": []interface{}{int64(1), int64(2)},
			"b": []byte{1, 2, 3},
		}},
	{"deep nest", "91 91 91 01",
		[]interface{}{[]interface{}{[]interface{}{int64(1)}}}},
	{"fixext4", "d6 05 01 02 03 04", Ext{Type: 5, Data: []byte{1, 2, 3, 4}}},
	{"timestamp32 ext", "d6 ff 65 0f 5a 00",
		Ext{Type: -1, Data: []byte{0x65, 0x0f, 0x5a, 0x00}}},
	{"ext8", "c7 03 07 aa bb cc", Ext{Type: 7, Data: []byte{0xaa, 0xbb, 0xcc}}},
}

// decodeOnlyFixtures are valid streams whose canonical re-encode differs
// (overlong headers, float32, etc.), so they are only checked on decode.
var decodeOnlyFixtures = []struct {
	name string
	hex  string
	want interface{}
}{
	{"str8", "d9 05 68 65 6c 6c 6f", "hello"},
	{"bin16", "c5 00 03 01 02 03", []byte{1, 2, 3}},
	{"array16", "dc 00 03 01 02 03",
		[]interface{}{int64(1), int64(2), int64(3)}},
	{"map16", "de 00 01 a1 61 01",
		map[interface{}]interface{}{"a": int64(1)}},
	{"float32", "ca 3f c0 00 00", float64(1.5)},
	{"str16", "da 00 05 68 65 6c 6c 6f", "hello"},
}

func TestDecodeFixtures(t *testing.T) {
	for _, f := range roundTripFixtures {
		t.Run(f.name, func(t *testing.T) {
			got, err := Unmarshal(mustHex(t, f.hex))
			if err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if !reflect.DeepEqual(got, f.want) {
				t.Fatalf("got %#v, want %#v", got, f.want)
			}
		})
	}
}

func TestEncodeFixtures(t *testing.T) {
	for _, f := range roundTripFixtures {
		t.Run(f.name, func(t *testing.T) {
			got, err := Marshal(f.want)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if hex.EncodeToString(got) != strings.ReplaceAll(f.hex, " ", "") {
				t.Fatalf("got %x, want %s", got, f.hex)
			}
		})
	}
}

func TestDecodeOnlyFixtures(t *testing.T) {
	for _, f := range decodeOnlyFixtures {
		t.Run(f.name, func(t *testing.T) {
			got, err := Unmarshal(mustHex(t, f.hex))
			if err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if !reflect.DeepEqual(got, f.want) {
				t.Fatalf("got %#v, want %#v", got, f.want)
			}
		})
	}
}

func TestDecodeOneByteAtATime(t *testing.T) {
	data := mustHex(t, "82 a1 61 92 01 02 a1 62 c4 03 01 02 03")
	got, err := NewDecoder(&oneByteReader{data: data}, nil).Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	want := map[interface{}]interface{}{
		"a": []interface{}{int64(1), int64(2)},
		"b": []byte{1, 2, 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestDecodeStreamMultipleValues(t *testing.T) {
	d := NewDecoder(bytes.NewReader(mustHex(t, "01 a1 61 c0")), nil)
	for i, want := range []interface{}{int64(1), "a", nil} {
		got, err := d.Decode()
		if err != nil {
			t.Fatalf("value %d: %v", i, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("value %d: got %#v, want %#v", i, got, want)
		}
	}
	if _, err := d.Decode(); err != io.EOF {
		t.Fatalf("stream end: got %v, want io.EOF", err)
	}
}

func TestDecodeErrors(t *testing.T) {
	cases := []struct {
		name string
		hex  string
		want error
	}{
		{"reserved 0xc1", "c1", ErrReservedFormat},
		{"reserved nested", "91 c1", ErrReservedFormat},
		{"bad utf8 fixstr", "a1 ff", ErrInvalidUTF8},
		{"bad utf8 str8", "d9 02 68 ff", ErrInvalidUTF8},
		{"truncated str payload", "a5 68 65", io.ErrUnexpectedEOF},
		{"truncated str header", "da 00", io.ErrUnexpectedEOF},
		{"truncated bin header", "c5 00", io.ErrUnexpectedEOF},
		{"truncated bin payload", "c4 03 01", io.ErrUnexpectedEOF},
		{"truncated float64", "cb 40 09", io.ErrUnexpectedEOF},
		{"truncated uint16", "cd 01", io.ErrUnexpectedEOF},
		{"truncated array", "93 01 02", io.ErrUnexpectedEOF},
		{"truncated map", "81 a1 61", io.ErrUnexpectedEOF},
		{"truncated ext payload", "d6 05 01", io.ErrUnexpectedEOF},
		{"empty input", "", io.EOF},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Unmarshal(mustHex(t, c.hex))
			if !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
}

func TestTrailingData(t *testing.T) {
	_, err := Unmarshal(mustHex(t, "01 02"))
	if !errors.Is(err, ErrTrailingData) {
		t.Fatalf("got %v, want ErrTrailingData", err)
	}
}

func TestDepthLimit(t *testing.T) {
	deep := strings.Repeat("91", 600) + "01"
	_, err := Unmarshal(mustHex(t, deep))
	if !errors.Is(err, ErrDepthLimit) {
		t.Fatalf("default limit: got %v, want ErrDepthLimit", err)
	}

	_, err = UnmarshalOptions(mustHex(t, "91 91 91 91 01"), &Options{MaxDepth: 3})
	if !errors.Is(err, ErrDepthLimit) {
		t.Fatalf("custom limit: got %v, want ErrDepthLimit", err)
	}

	v, err := UnmarshalOptions(mustHex(t, "91 91 91 01"), &Options{MaxDepth: 3})
	if err != nil {
		t.Fatalf("within limit: %v", err)
	}
	if !reflect.DeepEqual(v, []interface{}{[]interface{}{[]interface{}{int64(1)}}}) {
		t.Fatalf("within limit: got %#v", v)
	}
}

func TestSizeLimit(t *testing.T) {
	opts := &Options{MaxValueSize: 4}
	// Length is rejected before the payload is even read.
	for _, h := range []string{"d9 10", "c5 00 10", "c7 10 05"} {
		_, err := UnmarshalOptions(mustHex(t, h), opts)
		if !errors.Is(err, ErrSizeLimit) {
			t.Fatalf("%s: got %v, want ErrSizeLimit", h, err)
		}
	}
	v, err := UnmarshalOptions(mustHex(t, "a4 61 62 63 64"), opts)
	if err != nil || v != "abcd" {
		t.Fatalf("within limit: got %v, %v", v, err)
	}
}

func TestMapKeyRules(t *testing.T) {
	// Decode rejects non-string/non-int keys.
	for _, h := range []string{"81 c3 01", "81 cb 3f f0 00 00 00 00 00 00 01"} {
		_, err := Unmarshal(mustHex(t, h))
		if !errors.Is(err, ErrInvalidMapKey) {
			t.Fatalf("decode %s: got %v, want ErrInvalidMapKey", h, err)
		}
	}
	// Encode rejects them too.
	for _, m := range []map[interface{}]interface{}{
		{1.5: 1},
		{true: 1},
	} {
		if _, err := Marshal(m); !errors.Is(err, ErrInvalidMapKey) {
			t.Fatalf("encode %v: got %v, want ErrInvalidMapKey", m, err)
		}
	}
}

func TestMapCanonicalKeyOrder(t *testing.T) {
	m := map[interface{}]interface{}{
		int64(1):  "x",
		"a":       int64(2),
		int64(-1): int64(3),
	}
	// Canonical bytes: 1 -> 01, "a" -> a1 61, -1 -> ff; sorted bytewise.
	want := "83 01 a1 78 a1 61 02 ff 03"
	first, err := Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if hex.EncodeToString(first) != strings.ReplaceAll(want, " ", "") {
		t.Fatalf("got %x, want %s", first, want)
	}
	second, err := Marshal(m)
	if err != nil {
		t.Fatalf("Marshal again: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("not deterministic: %x vs %x", first, second)
	}
}

func TestEncodeStringKeyedMap(t *testing.T) {
	got, err := Marshal(map[string]interface{}{"b": int64(1), "a": int64(2)})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := "82 a1 61 02 a1 62 01"
	if hex.EncodeToString(got) != strings.ReplaceAll(want, " ", "") {
		t.Fatalf("got %x, want %s", got, want)
	}
}

func TestEncodeInvalidUTF8(t *testing.T) {
	if _, err := Marshal("\xff"); !errors.Is(err, ErrInvalidUTF8) {
		t.Fatalf("string: got %v, want ErrInvalidUTF8", err)
	}
	if _, err := Marshal(map[interface{}]interface{}{"\xff": 1}); !errors.Is(err, ErrInvalidUTF8) {
		t.Fatalf("map key: got %v, want ErrInvalidUTF8", err)
	}
}

func TestEncodeUnsupportedType(t *testing.T) {
	if _, err := Marshal(struct{}{}); !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("got %v, want ErrUnsupportedType", err)
	}
}

func TestExtRoundTrip(t *testing.T) {
	// Timestamp extension (type -1) passes through untouched.
	in := mustHex(t, "d7 ff 00 00 00 01 00 00 00 02")
	v, err := Unmarshal(in)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	x, ok := v.(Ext)
	if !ok || x.Type != -1 {
		t.Fatalf("got %#v, want Ext type -1", v)
	}
	out, err := Marshal(x)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Equal(out, in) {
		t.Fatalf("round trip: got %x, want %x", out, in)
	}
}

func TestEncodeToWriter(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.Encode(int64(1)); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode("a"); err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(buf.Bytes()) != "01a161" {
		t.Fatalf("got %x", buf.Bytes())
	}
}

func TestNonCanonicalIntsDecode(t *testing.T) {
	// Overlong encodings still decode to the same value.
	for h, want := range map[string]interface{}{
		"cc 01":                      int64(1),
		"cd 00 01":                   int64(1),
		"d1 ff ff":                   int64(-1),
		"d3 ff ff ff ff ff ff ff ff": int64(-1),
	} {
		got, err := Unmarshal(mustHex(t, h))
		if err != nil {
			t.Fatalf("%s: %v", h, err)
		}
		if got != want {
			t.Fatalf("%s: got %#v, want %#v", h, got, want)
		}
	}
}
