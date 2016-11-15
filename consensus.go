package libp2praft

import (
	"errors"

	consensus "gx/ipfs/QmZ88KbrvZMJpXaNwAGffswcYKz8EbeafzAFGMCA6MEZKt/go-libp2p-consensus"
)

// Consensus implements both the libp2p-consensus interface and the
// hashicorp/raft FSM interface.
type Consensus struct {
	fsm   // A consensus is a FSM in the end, in hashicorp/raft sense
	actor consensus.Actor
}

// NewConsensus returns a new consensus. Because the State
// is generic, and we know nothing about it, it needs to be initialized first.
// Note that this state initual state is not agreed upon in the cluster and that
// GetCurrentState() will return an error until a state is agreed upon. Only
// states submitted via CommitState() are agreed upon.
func NewConsensus(state consensus.State) *Consensus {
	con := new(Consensus)
	con.state = state
	con.valid = false
	return con
}

// SetActor changes the actor in charge of submitting new states to the system.
func (c *Consensus) SetActor(actor consensus.Actor) {
	c.actor = actor
}

// GetCurrentState returns the upon-agreed state of the system.
func (c *Consensus) GetCurrentState() (consensus.State, error) {
	return c.State()
}

// CommitState pushes a new state to the system and returns
// the state the system has agreed upon. It will block until
// that happens.
//
// Note that only the Raft leader can commit a state.
func (c *Consensus) CommitState(state consensus.State) (consensus.State, error) {
	if c.actor == nil {
		return nil, errors.New("no actor set to commit the new state")
	}
	return c.actor.SetState(state)
}
