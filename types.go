package msgpack

// Ext is a MessagePack extension value: a type code plus an opaque payload.
// Timestamp (type -1) is carried through this type unchanged.
type Ext struct {
	Type int8
	Data []byte
}
