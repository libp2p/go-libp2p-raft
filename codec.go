package libp2praft

import (
	"bytes"

	consensus "github.com/libp2p/go-libp2p-consensus"
	msgpack "github.com/multiformats/go-multicodec/msgpack"
)

// encodeState serializes a state
func encodeState(state stateWrapper) ([]byte, error) {
	buf := new(bytes.Buffer)
	enc := msgpack.Multicodec(msgpack.DefaultMsgpackHandle()).Encoder(buf)
	if err := enc.Encode(state); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decodeState deserializes a state
func decodeState(bs []byte, state *stateWrapper) error {
	buf := bytes.NewBuffer(bs)
	dec := msgpack.Multicodec(msgpack.DefaultMsgpackHandle()).Decoder(buf)
	if err := dec.Decode(state); err != nil {
		return err
	}
	return nil
}

// EncodeSnapshot serializes a state and is used by our raft's FSM
// implementation to describe the format raft stores snapshots on
// disk.
func EncodeSnapshot(state consensus.State) ([]byte, error) {
	marshable, ok := state.(MarshableState)
	if ok {
		return marshable.Marshal()
	}
	return encodeState(stateWrapper{state})
}

// DecodeSnapshot de-serializes a state encoded with EncodeSnapshot
// onto the given state. It is used by our raft's FSM implementation
// which allows raft to read snapshots.
func DecodeSnapshot(snap []byte, state consensus.State) error {
	marshable, ok := state.(MarshableState)
	if ok {
		return marshable.Unmarshal(snap)
	}
	return decodeState(snap, &stateWrapper{state})
}

// encodeOp serializes an op
func encodeOp(op opWrapper) ([]byte, error) {
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
func decodeOp(bs []byte, op *opWrapper) (err error) {
	buf := bytes.NewBuffer(bs)
	h := msgpack.DefaultMsgpackHandle()
	h.ErrorIfNoField = true
	dec := msgpack.Multicodec(h).Decoder(buf)
	if err = dec.Decode(op); err != nil {
		return err
	}
	return nil
}
