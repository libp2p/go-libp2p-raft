package libp2praft

import (
	"errors"
	"io"
	"io/ioutil"
	"sync"

	consensus "github.com/libp2p/go-libp2p-consensus"

	"github.com/hashicorp/raft"
	codec "github.com/ugorji/go/codec"
)

var MaxSubscriberCh = 128

// fsm implements a minimal raft.FSM that holds a generic consensus.State
// so it can be serialized/deserialized, snappshotted and restored.
// fsm is used by Consensus to keep track of the state of the system
type fsm struct {
	state consensus.State
	valid bool
	mux   sync.Mutex

	subscriberCh chan consensus.State
	chMux        sync.Mutex
}

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

// Returns a new state which is a copy of the given one.
// In order to copy it it serializes and deserializes it into a new
// variable.
func dupState(state consensus.State) (consensus.State, error) {
	newState := state

	// We encode and redecode on the new object
	bs, err := encodeState(state)
	if err != nil {
		return nil, err
	}

	err = decodeState(bs, &newState)
	if err != nil {
		return nil, err
	}

	return newState, nil
}

// Apply is invoked once a log entry is commited. It deserializes a Raft log
// entry and saves it as our new state which is returned. The new state
// is then used by Raft to create the future which is returned by Raft.Apply()
// The future is a copy of the fsm state so that the fsm suffers no side
// effects from external modifications.
func (fsm *fsm) Apply(rlog *raft.Log) interface{} {
	fsm.mux.Lock()
	defer fsm.mux.Unlock()

	if err := decodeState(rlog.Data, &fsm.state); err != nil {
		logger.Error("error decoding state: ", err)
		return nil
	}

	newState, err := dupState(fsm.state)
	if err != nil {
		logger.Error("error duplicating state to return it as future:", err)
		return nil
	}
	fsm.valid = true

	fsm.updateSubscribers(newState)

	return newState
}

func (fsm *fsm) Snapshot() (raft.FSMSnapshot, error) {
	fsm.mux.Lock()
	defer fsm.mux.Unlock()

	// Encode the state
	bytes, err := encodeState(fsm.state)
	if err != nil {
		return nil, err
	}

	var snap fsmSnapshot = bytes
	return snap, nil
}

func (fsm *fsm) Restore(reader io.ReadCloser) error {
	snapBytes, err := ioutil.ReadAll(reader)
	if err != nil {
		logger.Errorf("Error reading snapshot: %s", err)
		return err
	}

	fsm.mux.Lock()
	defer fsm.mux.Unlock()
	if err := decodeState(snapBytes, &fsm.state); err != nil {
		logger.Errorf("Error decoding snapshot: %s", err)
		return err
	}
	fsm.valid = true
	return nil
}

// State returns a copy of the state so that the fsm cannot be
// messed with if the state is modified outside
func (fsm *fsm) State() (consensus.State, error) {
	fsm.mux.Lock()
	defer fsm.mux.Unlock()
	if fsm.valid != true {
		return nil, errors.New("no state has been agreed upon yet")
	}
	return dupState(fsm.state)
}

func (fsm *fsm) updateSubscribers(st consensus.State) {
	fsm.chMux.Lock()
	defer fsm.chMux.Unlock()
	if fsm.subscriberCh != nil {
		select {
		case fsm.subscriberCh <- st:
		default:
			logger.Error("subscriber channel is full. Discarding state!")
		}
	}
}

// fsmSnapshot implements the hashicorp/raft interface and allows to serialize a
// state into a byte slice that can be used as a snapshot of the system.
type fsmSnapshot []byte

// Persist writes the snapshot (a serialized state) to a raft.SnapshotSink
func (fsms fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	_, err := sink.Write(fsms)
	if err != nil {
		return err
	}
	if err := sink.Close(); err != nil {
		return err
	}
	return nil
}

func (fsms fsmSnapshot) Release() {
}
