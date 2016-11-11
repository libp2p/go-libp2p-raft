package libp2praft

import (
	"errors"
	"time"

	"github.com/hashicorp/raft"
	consensus "github.com/libp2p/go-libp2p-consensus"
)

// SetStateTimeout specifies how long before giving up on setting a state
var SetStateTimeout time.Duration = 5 * time.Second

// Actor implements a consensus.Actor, allowing to SetState
// in a libp2p Consensus system. In order to do this it uses hashicorp/raft
// implementation of the Raft algorithm.
type Actor struct {
	Raft *raft.Raft
}

// SetState attempts to set the state of the cluster to the state
// represented by the given Node. It will block until a state is
// agreed upon, and will return then the new state.
//
// Only the Raft leader can set the state. Otherwise, an error will
// be returned.
func (actor Actor) SetState(newState consensus.State) (consensus.State, error) {
	//log.Debug("Actor is applying state")
	if actor.Raft == nil {
		return nil, errors.New("this actor does not have a raft instance")
	}

	if actor.Raft.State() != raft.Leader {
		return nil, errors.New("this actor is not the leader")
	}

	bs, err := encodeState(newState)
	if err != nil {
		return nil, err
	}

	applyFuture := actor.Raft.Apply(bs, SetStateTimeout)

	// Error blocks until apply future has returned
	// which would mean the state has been agreed upon?
	err = applyFuture.Error()

	futureResp := applyFuture.Response()
	//log.Debugf("Apply future log entry index: %d", applyFuture.Index())
	return futureResp, nil
}

// NewActor returns a new actor given a hashicorp/raft node.
func NewActor(r *raft.Raft) Actor {
	return Actor{
		Raft: r,
	}
}
