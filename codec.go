package libp2praft

import (
	"bytes"

	consensus "github.com/libp2p/go-libp2p-consensus"
	msgpack "github.com/multiformats/go-multicodec/msgpack"
)

// encodeState serializes a state
func encodeState(state consensus.State) ([]byte, error) {
	buf := new(bytes.Buffer)
	enc := msgpack.Multicodec(msgpack.DefaultMsgpackHandle()).Encoder(buf)
	if err := enc.Encode(state); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decodeState deserializes a state
func decodeState(bs []byte, state *consensus.State) error {
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

// decodeOp deserializes a op
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
