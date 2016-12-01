package libp2praft

import (
	consensus "github.com/libp2p/go-libp2p-consensus"
	"github.com/ugorji/go/codec"
)

// encodeState serializes a state
func encodeState(state consensus.State) ([]byte, error) {
	var buf []byte
	enc := codec.NewEncoderBytes(&buf, &codec.MsgpackHandle{})
	if err := enc.Encode(state); err != nil {
		return nil, err
	}
	// enc := msgpack.Multicodec().NewEncoder(buffer)
	// if err := enc.Encode(state); err != nil {
	// 	return nil, err
	// }
	return buf, nil
}

// decodeState deserializes a state
func decodeState(bs []byte, state *consensus.State) error {
	dec := codec.NewDecoderBytes(bs, &codec.MsgpackHandle{})

	if err := dec.Decode(state); err != nil {
		return err
	}

	// buffer := bytes.NewBuffer(bs)
	// dec := msgpack.MultiCodec().NewDecoder(buffer)
	// if err := dec.Decode(state); err != nil {
	// 	return err
	// }
	return nil
}

// encodeOp serializes an op
func encodeOp(op consensus.Op) ([]byte, error) {
	var buf []byte
	enc := codec.NewEncoderBytes(&buf, &codec.MsgpackHandle{})
	if err := enc.Encode(op); err != nil {
		return nil, err
	}

	// enc := msgpack.Multicodec().NewEncoder(buffer)
	// if err := enc.Encode(state); err != nil {
	// 	return nil, err
	// }
	return buf, nil
}

// decodeOp deserializes a op
func decodeOp(bs []byte, op *consensus.Op) error {
	dec := codec.NewDecoderBytes(bs, &codec.MsgpackHandle{})

	if err := dec.Decode(op); err != nil {
		return err
	}

	// buffer := bytes.NewBuffer(bs)
	// dec := msgpack.MultiCodec().NewDecoder(buffer)
	// if err := dec.Decode(state); err != nil {
	// 	return err
	// }
	return nil
}
