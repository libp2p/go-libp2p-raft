package libp2praft

import (
	"bytes"

	consensus "github.com/libp2p/go-libp2p-consensus"
	msgpack "github.com/multiformats/go-multicodec/msgpack"
)

// EncodeState serializes a state
func EncodeState(state consensus.State) ([]byte, error) {
	buf := new(bytes.Buffer)
	enc := msgpack.Multicodec(msgpack.DefaultMsgpackHandle()).Encoder(buf)
	if err := enc.Encode(state); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeState deserializes a state
func DecodeState(bs []byte, state *consensus.State) error {
	buf := bytes.NewBuffer(bs)
	dec := msgpack.Multicodec(msgpack.DefaultMsgpackHandle()).Decoder(buf)
	if err := dec.Decode(state); err != nil {
		return err
	}
	return nil
}

// encodeOp serializes an op
func encodeOp(op consensus.Op) ([]byte, error) {
	buf := new(bytes.Buffer)
	enc := msgpack.Multicodec(msgpack.DefaultMsgpackHandle()).Encoder(buf)
	if err := enc.Encode(op); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decodeOp deserializes an op
// ErrorIfNoField needs to be true so we can identify between standard
// operations and rollback operations in fsm.Apply(). If this did not fail
// we'd have to find a different way of identifying a rollback.
func decodeOp(bs []byte, op *consensus.Op) (err error) {
	buf := bytes.NewBuffer(bs)
	h := msgpack.DefaultMsgpackHandle()
	h.ErrorIfNoField = true
	dec := msgpack.Multicodec(h).Decoder(buf)
	if err = dec.Decode(op); err != nil {
		return err
	}
	return nil
}
