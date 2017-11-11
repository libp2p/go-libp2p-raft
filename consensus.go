package libp2praft

import (
	"errors"

	consensus "github.com/libp2p/go-libp2p-consensus"
)

// Consensus implements both the go-libp2p-consensus Consensus
// and the OpLogConsensus interfaces. This is because the Consensus
// interface is just a particular case of the OpLog interface,
// where the operation applied holds the new version of the state
// and replaces the current one with it.
type Consensus struct {
	fsm   *FSM
	actor consensus.Actor
}

// MarshableState is an interface to be implemented by those States that
// wish to choose their serialization format for Raft snapshots.
// libp2praft will check if the consensus.State it is working with
// implements the MarshableState interface and, if so, will call Marshal()
// and Unmarshal() when doing snapshots and restoring them. Otherwise,
// a default serialization strategy will be used.
//
// MarshableState is useful to allow the user-provided consensus.State to
// decide which format Raft snapshots use.
type MarshableState interface {
	Marshal() ([]byte, error)
	Unmarshal([]byte) error
}

// stateOp: A Consensus is a particular case of OpLogConsensus where the
// Operation is itself the state and every new operation just replaces the
// previous state. For this case, we use this operation. It tracks the state
// itself. It is also used for performing rollbacks to a given state.
type stateOp struct {
	State consensus.State
}

// ApplyTo just returns the state in the operation and discards the
// "old" one.
func (sop stateOp) ApplyTo(st consensus.State) (consensus.State, error) {
	return sop.State, nil
}

// NewConsensus returns a new consensus. The state is provided so that
// the appropiate internal structures can be initialized. The underlying
// State type should be a pointer, otherwise some operations will not work.
//
// Note that this initial state is not agreed upon in the cluster and that
// GetCurrentState() will return an error until a state is agreed upon. Only
// states submitted via CommitState() are agreed upon.
func NewConsensus(state consensus.State) *Consensus {
	return NewOpLog(state, stateOp{State: state})
}

// NewOpLog returns a new OpLog. Because the State and the Ops
// are generic, and we know nothing about them, they need to
// be provided in order to initialize internal structures with the
// right types. Both the state and the op parameters are not used nor
// agreed upon just yet. Both the state and the op underlying types should
// be pointers to something, otherwise expect some misbehaviours.
//
// It is important to note that the system agrees on an operation log,
// but the resulting state can only be considered agreed-upon if all
// the operations in the log can be or were successfully applied to it
// (with Op.ApplyTo()).
// See the notes in CommitOp() for more information.
func NewOpLog(state consensus.State, op consensus.Op) *Consensus {
	return &Consensus{
		fsm: &FSM{
			stateWrap:    stateWrapper{state},
			opWrap:       opWrapper{op},
			initialized:  false,
			inconsistent: false,
			subscriberCh: nil,
		},
		actor: nil,
	}
}

// FSM returns the raft.FSM implementation provided by go-libp2p-raft. It is
// necessary to initialize raft with this FSM.
func (c *Consensus) FSM() *FSM {
	return c.fsm
}

// SetActor changes the actor in charge of submitting new states to the system.
func (c *Consensus) SetActor(actor consensus.Actor) {
	c.actor = actor
}

// GetCurrentState returns the upon-agreed state of the system. It will
// return an error when no state has been agreed upon or when the state
// cannot be ensured to be that on which the rest of the system has
// agreed-upon. The underlying state will be the same as provided
// initialization.
func (c *Consensus) GetCurrentState() (consensus.State, error) {
	return c.GetLogHead()
}

// CommitOp submits a new operation to the system. If the operation is
// agreed-upon, then and only then will it call ApplyTo() and modify the
// current state (log head).
//
// If ApplyTo() fails, the operation stays nevertheless
// in the log, but the state of the system is marked as invalid and
// nil is returned. From that point the state is marked as inconsistent
// and calls to GetLogHead() will fail for this node, even though updates
// will be processed as usual.
//
// Inconsistent states can be rescued using Rollback().
// The underlying object to the returned State will be the one provided
// during initialization.
func (opLog *Consensus) CommitOp(op consensus.Op) (consensus.State, error) {
	if opLog.actor == nil {
		return nil, errors.New("no actor set to commit the new state")
	}
	newSt, err := opLog.actor.SetState(op)
	if err != nil {
		return nil, err
	}
	return newSt, nil
}

// GetLogHead returns the newest known state of the log. It will
// return an error when no state has been agreed upon or when the state
// cannot be ensured to be that on which the rest of the system has
// agreed-upon.
func (opLog *Consensus) GetLogHead() (consensus.State, error) {
	return opLog.fsm.getState()
}

// CommitState pushes a new state to the system and returns
// the state the system has agreed upon. It will block until
// that happens.
//
// Note that only the Raft leader can commit a state.
func (c *Consensus) CommitState(state consensus.State) (consensus.State, error) {
	return c.CommitOp(stateOp{state})
}

// Rollback hammers the provided state into the system. It does not un-do any
// operations nor checks that the given state was previously agreed-upon. A
// special rollback operation gets added to the log, like any other operation.
// A successful rollback marks an inconsistent state as valid again.
func (opLog *Consensus) Rollback(state consensus.State) error {
	_, err := opLog.CommitState(state)
	return err
}

// subscribe returns a channel on which every new state is sent.
func (c *Consensus) Subscribe() <-chan consensus.State {
	return c.fsm.subscribe()
}

// unsubscribe closes the channel returned upon Subscribe() (if any).
func (c *Consensus) Unsubscribe() {
	c.fsm.unsubscribe()
}
