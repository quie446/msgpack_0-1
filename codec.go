package msgpack

import (
	"bytes"
	"fmt"
)

// Marshal encodes v to its canonical MessagePack form.
func Marshal(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	if err := NewEncoder(&buf).Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Unmarshal decodes exactly one value from data. Extra bytes after the
// value are reported as ErrTrailingData.
func Unmarshal(data []byte) (interface{}, error) {
	return UnmarshalOptions(data, nil)
}

// UnmarshalOptions is Unmarshal with configurable decoder limits.
func UnmarshalOptions(data []byte, opts *Options) (interface{}, error) {
	r := bytes.NewReader(data)
	v, err := NewDecoder(r, opts).Decode()
	if err != nil {
		return nil, err
	}
	if r.Len() > 0 {
		return nil, fmt.Errorf("%w: %d byte(s) left", ErrTrailingData, r.Len())
	}
	return v, nil
}
