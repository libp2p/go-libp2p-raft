package libp2praft

import (
	"fmt"
	"os"
	"testing"
	"time"
)

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

func TestSubscribe(t *testing.T) {
	peer1, _ := NewRandomPeer(9997)
	peer2, _ := NewRandomPeer(9998)
	peers1 := []*Peer{peer2}
	peers2 := []*Peer{peer1}

	raft1, c1, tr1, err := makeTestingRaft(peer1, peers1, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer raft1.Shutdown()
	defer tr1.Close()
	raft2, c2, tr2, err := makeTestingRaft(peer2, peers2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer raft2.Shutdown()
	defer tr2.Close()
	defer os.RemoveAll(raftTmpFolder)

	actor1 := NewActor(raft1)
	actor2 := NewActor(raft2)

	c1.SetActor(actor1)
	c2.SetActor(actor2)

	subscriber1 := c1.Subscribe()
	subscriber2 := c2.Subscribe()
	c1.Subscribe() // cover multiple calls to subscribe
	c2.Subscribe()

	time.Sleep(2 * time.Second)

	if !actor1.IsLeader() && !actor2.IsLeader() {
		t.Fatal("raft failed to declare a leader")
	}

	updateState := func(c *Consensus) {
		for i := 0; i < 5; i++ {
			c.CommitState(raftState{fmt.Sprintf("%d", i)})
		}
	}

	// On of these is going just not update because it's not the leader
	updateState(c1)
	updateState(c2)

	time.Sleep(2 * time.Second)

	// Check subscriber 1 got all the updates and not more
	for i := 0; i < 10; i++ {
		select {
		case st := <-subscriber1:
			newSt := st.(raftState)
			t.Log("Received state:", newSt.Msg)
			if newSt.Msg != fmt.Sprintf("%d", i) {
				t.Fatal("Expected a different state")
			}
		default:
			if i < 5 {
				t.Fatal("Expected to read something")
			} else {
				t.Log("subscriber1 channel is empty")
			}
		}
	}

	// Check subscriber 2 got all the updates and not more
	for i := 0; i < 10; i++ {
		select {
		case st := <-subscriber2:
			newSt := st.(raftState)
			t.Log("Received state:", newSt.Msg)
			if newSt.Msg != fmt.Sprintf("%d", i) {
				t.Fatal("Expected a different state")
			}
		default:
			if i < 5 {
				t.Fatal("Expected to read something")
			} else {
				t.Log("subscriber2 channel is empty")
			}
		}
	}

	// Cover multiple unsubscribes
	c1.Unsubscribe()
	c2.Unsubscribe()
	c1.Unsubscribe()
	c2.Unsubscribe()
}

func TestOpLog(t *testing.T) {
	peer1, _ := NewRandomPeer(9997)
	peer2, _ := NewRandomPeer(9998)
	peers1 := []*Peer{peer2}
	peers2 := []*Peer{peer1}

	raft1, opLog1, tr1, err := makeTestingRaft(peer1, peers1, testOperation{})
	if err != nil {
		t.Fatal(err)
	}
	defer raft1.Shutdown()
	defer tr1.Close()
	raft2, opLog2, tr2, err := makeTestingRaft(peer2, peers2, testOperation{})
	if err != nil {
		t.Fatal(err)
	}
	defer raft2.Shutdown()
	defer tr2.Close()
	defer os.RemoveAll(raftTmpFolder)

	actor1 := NewActor(raft1)
	actor2 := NewActor(raft2)
	opLog1.SetActor(actor1)
	opLog2.SetActor(actor2)

	time.Sleep(3 * time.Second)

	if !actor1.IsLeader() && !actor2.IsLeader() {
		t.Fatal("raft failed to declare a leader")
	}

	testCommitOps := func(opLog *Consensus) {
		op := testOperation{"I have "}
		opLog.CommitOp(op)
		op = testOperation{"appended "}
		opLog.CommitOp(op)
		op = testOperation{"this sentence "}
		opLog.CommitOp(op)
		op = testOperation{"to the state Msg."}
		opLog.CommitOp(op)
	}

	// Only leader will succeed
	t.Log("testing CommitOp")
	testCommitOps(opLog1)
	testCommitOps(opLog2)

	time.Sleep(1 * time.Second)

	logHead1, err := opLog1.GetLogHead()
	if err != nil {
		t.Fatal(err)
	}

	logHead2, err := opLog2.GetLogHead()
	if err != nil {
		t.Fatal(err)
	}

	newSt1 := logHead1.(raftState)
	t.Log(newSt1.Msg)
	if newSt1.Msg != "I have appended this sentence to the state Msg." {
		t.Error("Log head is not the result of applying the operations")
	}

	newSt2 := logHead2.(raftState)
	t.Log(newSt2.Msg)
	if newSt2.Msg != "I have appended this sentence to the state Msg." {
		t.Error("Log head is not the result of applying the operations")
	}

	// Test a ROLLBACK now
	// Only the leader will succeed
	t.Log("testing Rollback")
	opLog1.Rollback(raftState{"Good as new"})
	opLog2.Rollback(raftState{"Good as new"})

	time.Sleep(1 * time.Second)

	logHead1, err = opLog1.GetLogHead()
	if err != nil {
		t.Fatal(err)
	}

	logHead2, err = opLog2.GetLogHead()
	if err != nil {
		t.Fatal(err)
	}

	newSt1 = logHead1.(raftState)
	t.Log(newSt1.Msg)
	if newSt1.Msg != "Good as new" {
		t.Error("Log head is not the result of a rollback")
	}

	newSt2 = logHead2.(raftState)
	t.Log(newSt2.Msg)
	if newSt2.Msg != "Good as new" {
		t.Error("Log head is not the result of a rollback")
	}
}
