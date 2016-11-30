package libp2praft

import (
	"errors"

	consensus "github.com/libp2p/go-libp2p-consensus"
)

// Consensus implements both the go-libp2p-consensus Consensus
// and the OpLogConsensus interfaces. The Consensus interface can be
// expressed as a particular case of a distributed OpLog, where every
// operation is the state itself. Therefore, it is natural and cheap
// to implement both in the same place.
//
// Furthermore, the Consensus type implements the Hashicorp
// Raft FSM interface. It should be used to initialize Raft, which
// can be later used as an Actor.
type Consensus struct {
	fsm
	actor consensus.Actor
}

// A Consensus is a particular case of OpLogConsensus where the Operation
// is itself the state and every new operation just replaces the previous
// state. For this case, we use an operation which tracks the state itself.
type consensusOp struct {
	State consensus.State
}

// ApplyTo just returns the state in the operation and discards the
// "old" one.
func (cop consensusOp) ApplyTo(st consensus.State) (consensus.State, error) {
	return cop.State, nil
}

// NewConsensus returns a new consensus. Because the State
// is generic, and we know nothing about it, it needs to be initialized first.
// Note that this state initual state is not agreed upon in the cluster and that
// GetCurrentState() will return an error until a state is agreed upon. Only
// states submitted via CommitState() are agreed upon.
func NewConsensus(state consensus.State) *Consensus {
	return NewOpLog(state, consensusOp{State: state})
}

// NewOpLog returns a new OpLog. Because the State and the Ops
// are generic, and we know nothing about them, they need to
// be provided in order to initialize internal structures with the
// right types. Both the state and the op are not used nor agreed upon.
// Only ops commited via CommitOp change the state of the system.
func NewOpLog(state consensus.State, op consensus.Op) *Consensus {
	con := new(Consensus)
	con.state = state
	con.op = op
	con.valid = false
	con.subscriberCh = nil
	return con
}

// SetActor changes the actor in charge of submitting new states to the system.
func (c *Consensus) SetActor(actor consensus.Actor) {
	c.actor = actor
}

// GetCurrentState returns the upon-agreed state of the system.
func (c *Consensus) GetCurrentState() (consensus.State, error) {
	return c.GetLogHead()
}

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

func (opLog *Consensus) GetLogHead() (consensus.State, error) {
	return opLog.State()
}

// CommitState pushes a new state to the system and returns
// the state the system has agreed upon. It will block until
// that happens.
//
// Note that only the Raft leader can commit a state.
func (c *Consensus) CommitState(state consensus.State) (consensus.State, error) {
	return c.CommitOp(consensusOp{state})
}

func (opLog *Consensus) Rollback(state consensus.State) error {
	return errors.New("Not implemented")
}
