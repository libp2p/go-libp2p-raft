package libp2praft

import "testing"

// TestNewConsensus sees that a new consensus object works as expected
func TestNewConsensus(t *testing.T) {
	type myState struct {
		Msg string
	}

	state := myState{
		"we are testing",
	}

	con := NewConsensus(state)

	st, err := con.GetCurrentState()
	if st != nil || err == nil {
		t.Error("GetCurrentState() should error if state is not valid")
	}

	st, err = con.CommitState(state)
	if st != nil || err == nil {
		t.Error("CommitState() should error if no actor is set")
	}
}
