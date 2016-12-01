package libp2praft

import (
	"errors"
	"io"
	"io/ioutil"
	"sync"

	consensus "github.com/libp2p/go-libp2p-consensus"

	"github.com/hashicorp/raft"
)

var MaxSubscriberCh = 128

// fsm implements a minimal raft.FSM that holds a generic consensus.State
// and applies generic Ops to it. The state can be serialized/deserialized,
// snappshotted and restored.
// fsm is used by Consensus to keep track of the state of an OpLog.
type fsm struct {
	state consensus.State
	op    consensus.Op
	valid bool
	mux   sync.Mutex

	subscriberCh chan consensus.State
	chMux        sync.Mutex
}

// Apply is invoked once a log entry is commited. It deserializes a Raft log
// entry, creates an operation with it, applies it to the current state and
// saves it as our new state which is returned.
// It is then used by Raft to create the future which is returned by
// Raft.Apply().
func (fsm *fsm) Apply(rlog *raft.Log) interface{} {
	fsm.mux.Lock()
	defer fsm.mux.Unlock()

	if err := decodeOp(rlog.Data, &fsm.op); err != nil {
		logger.Error("error decoding op: ", err)
		return nil
	}

	//fmt.Printf("%+v\n", fsm.op)
	newState, err := fsm.op.ApplyTo(fsm.state)
	if err != nil {
		// FIXME: OMG what happens if this actually fails!
		logger.Error("error applying Op to State")
	} else {
		fsm.state = newState
		fsm.valid = true
	}
	//fmt.Printf("%+v\n", fsm.state)

	fsm.updateSubscribers(fsm.state)
	return fsm.state
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

// Subscribe returns a channel on which every new state is sent.
func (fsm *fsm) Subscribe() <-chan consensus.State {
	fsm.chMux.Lock()
	defer fsm.chMux.Unlock()
	if fsm.subscriberCh == nil {
		fsm.subscriberCh = make(chan consensus.State, MaxSubscriberCh)
	}
	return fsm.subscriberCh
}

// Unsubscribe closes the channel returned upon Subscribe() (if any).
func (fsm *fsm) Unsubscribe() {
	fsm.chMux.Lock()
	defer fsm.chMux.Unlock()
	if fsm.subscriberCh != nil {
		close(fsm.subscriberCh)
		fsm.subscriberCh = nil
	}
}

// State returns the current state as agreed by the cluster
func (fsm *fsm) State() (consensus.State, error) {
	fsm.mux.Lock()
	defer fsm.mux.Unlock()
	if fsm.valid != true {
		return nil, errors.New("no state has been agreed upon yet")
	}
	return fsm.state, nil
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
