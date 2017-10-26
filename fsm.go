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

// FSM implements a minimal raft.FSM that holds a generic consensus.State
// and applies generic Ops to it. The state can be serialized/deserialized,
// snappshotted and restored.
// FSM is used by Consensus to keep track of the state of an OpLog.
// Please use the value returned by Consensus.FSM() to initialize Raft.
// Do not use this object directly.
type FSM struct {
	state        consensus.State
	op           consensus.Op
	initialized  bool
	inconsistent bool

	mux sync.Mutex

	subscriberCh chan consensus.State
	chMux        sync.Mutex
}

// Apply is invoked by Raft once a log entry is commited. Do not use directly.
func (fsm *FSM) Apply(rlog *raft.Log) interface{} {
	fsm.mux.Lock()
	defer fsm.mux.Unlock()

	// What this does:
	// - Check that we can deserialize an operation
	//   - If no -> check if we can deserialize a stateOp (rollbacks)
	//     - If it fails -> return nil and mark the state inconsistent
	//     - If it works -> replace the state and mark it as consistent
	//   - If yes -> ApplyTo() the state if it is consistent so far
	//     - If ApplyTo() fails, return nil and mark state as inconsistent
	//     - If ApplyTo() works, replace the state
	// - Notify subscribers of the new state and return it

	var newState consensus.State

	if err := decodeOp(rlog.Data, &fsm.op); err != nil {
		// maybe it is a standard rollback
		rollbackOp := consensus.Op(stateOp{fsm.state})
		err := decodeOp(rlog.Data, &rollbackOp)
		if err != nil {
			logger.Error("error decoding op: ", err)
			fsm.inconsistent = true
			return nil
		}
		//fmt.Printf("%+v\n", rollbackOp)
		castedRollback := rollbackOp.(stateOp)
		newState = castedRollback.State
		fsm.inconsistent = false
	} else {
		//fmt.Printf("%+v\n", fsm.op)
		newState, err = fsm.op.ApplyTo(fsm.state)
		if err != nil {
			logger.Error("error applying Op to State")
			fsm.inconsistent = true
			return nil
		}
	}
	fsm.state = newState
	fsm.initialized = true

	fsm.updateSubscribers(fsm.state)
	return fsm.state
}

func (fsm *FSM) Snapshot() (raft.FSMSnapshot, error) {
	fsm.mux.Lock()
	defer fsm.mux.Unlock()
	if !fsm.initialized {
		logger.Error("tried to snapshot uninitialized state")
		return nil, errors.New("cannot snapshot unitialized state")
	}
	if fsm.inconsistent {
		logger.Error("tried to snapshot inconsistent state")
		return nil, errors.New("cannot snapshot inconsistent state")
	}

	// Encode the state
	bytes, err := encodeState(fsm.state)
	if err != nil {
		return nil, err
	}

	var snap fsmSnapshot = bytes
	return snap, nil
}

func (fsm *FSM) Restore(reader io.ReadCloser) error {
	snapBytes, err := ioutil.ReadAll(reader)
	if err != nil {
		logger.Errorf("error reading snapshot: %s", err)
		return err
	}

	fsm.mux.Lock()
	defer fsm.mux.Unlock()
	if err := decodeState(snapBytes, &fsm.state); err != nil {
		logger.Errorf("error decoding snapshot: %s", err)
		return err
	}
	fsm.initialized = true
	fsm.inconsistent = false
	return nil
}

// subscribe returns a channel on which every new state is sent.
func (fsm *FSM) subscribe() <-chan consensus.State {
	fsm.chMux.Lock()
	defer fsm.chMux.Unlock()
	if fsm.subscriberCh == nil {
		fsm.subscriberCh = make(chan consensus.State, MaxSubscriberCh)
	}
	return fsm.subscriberCh
}

// unsubscribe closes the channel returned upon Subscribe() (if any).
func (fsm *FSM) unsubscribe() {
	fsm.chMux.Lock()
	defer fsm.chMux.Unlock()
	if fsm.subscriberCh != nil {
		close(fsm.subscriberCh)
		fsm.subscriberCh = nil
	}
}

// getState returns the current state as agreed by the cluster
func (fsm *FSM) getState() (consensus.State, error) {
	fsm.mux.Lock()
	defer fsm.mux.Unlock()
	if !fsm.initialized {
		return nil, errors.New("no state has been agreed upon yet")
	}
	if fsm.inconsistent {
		return nil, errors.New("the state on this node is not consistent")
	}
	return fsm.state, nil
}

func (fsm *FSM) updateSubscribers(st consensus.State) {
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
