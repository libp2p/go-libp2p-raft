package libp2praft

import (
	"errors"
	"fmt"

	"os"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	consensus "github.com/libp2p/go-libp2p-consensus"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
)

// newRandomHost returns a peer which listens on the given tcp port
// on localhost.
func newRandomHost(listenPort int, t *testing.T) host.Host {
	h, err := libp2p.New(
		libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", listenPort)),
	)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func makeTwoPeers(t *testing.T) (h1 host.Host, h2 host.Host, pids []peer.ID) {
	h1 = newRandomHost(9997, t)
	h2 = newRandomHost(9998, t)
	h1.Peerstore().AddAddrs(h2.ID(), h2.Addrs(), peerstore.PermanentAddrTTL)
	h2.Peerstore().AddAddrs(h1.ID(), h1.Addrs(), peerstore.PermanentAddrTTL)
	pids = []peer.ID{h1.ID(), h2.ID()}
	return
}

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
	peer1, peer2, pids := makeTwoPeers(t)
	defer peer1.Close()
	defer peer2.Close()
	raft1, c1, tr1 := makeTestingRaft(t, peer1, pids, nil)
	defer shutdown(t, raft1)
	defer tr1.Close()
	raft2, c2, tr2 := makeTestingRaft(t, peer2, pids, nil)
	defer shutdown(t, raft2)
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

	waitForLeader(t, raft1)

	updateState := func(c *Consensus) {
		for i := 0; i < 5; i++ {
			c.CommitState(&raftState{fmt.Sprintf("%d", i)})
		}
	}

	// On of these is going just not update because it's not the leader
	updateState(c1)
	updateState(c2)

	time.Sleep(2 * time.Second)

	// Check subscriber 1 got all the updates and not more
	for i := 0; i < 5; i++ {
		select {
		case <-subscriber1:
		default:
			if i < 5 {
				t.Fatal("expected to read something")
			} else {
				t.Log("subscriber1 channel is empty")
			}
		}
	}

	// Check subscriber 2 got all the updates and not more
	for i := 0; i < 5; i++ {
		select {
		case <-subscriber2:
		default:
			if i < 5 {
				t.Fatal("expected to read something")
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
	peer1, peer2, pids := makeTwoPeers(t)
	defer peer1.Close()
	defer peer2.Close()
	raft1, opLog1, tr1 := makeTestingRaft(t, peer1, pids, &testOperation{})
	defer shutdown(t, raft1)
	defer tr1.Close()
	raft2, opLog2, tr2 := makeTestingRaft(t, peer2, pids, &testOperation{})
	defer shutdown(t, raft2)
	defer tr2.Close()
	defer os.RemoveAll(raftTmpFolder)

	actor1 := NewActor(raft1)
	actor2 := NewActor(raft2)
	opLog1.SetActor(actor1)
	opLog2.SetActor(actor2)

	waitForLeader(t, raft1)

	testCommitOps := func(opLog *Consensus) {
		op := &testOperation{"I have "}
		opLog.CommitOp(op)
		op = &testOperation{"appended "}
		opLog.CommitOp(op)
		op = &testOperation{"this sentence "}
		opLog.CommitOp(op)
		op = &testOperation{"to the state Msg."}
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

	newSt1 := logHead1.(*raftState)
	t.Log(newSt1.Msg)
	if newSt1.Msg != "I have appended this sentence to the state Msg." {
		t.Error("Log head is not the result of applying the operations")
	}

	newSt2 := logHead2.(*raftState)
	t.Log(newSt2.Msg)
	if newSt2.Msg != "I have appended this sentence to the state Msg." {
		t.Error("Log head is not the result of applying the operations")
	}

	// Test a ROLLBACK now
	// Only the leader will succeed
	t.Log("testing Rollback")
	opLog1.Rollback(&raftState{"Good as new"})
	opLog2.Rollback(&raftState{"Good as new"})

	time.Sleep(1 * time.Second)

	logHead1, err = opLog1.GetLogHead()
	if err != nil {
		t.Fatal(err)
	}

	logHead2, err = opLog2.GetLogHead()
	if err != nil {
		t.Fatal(err)
	}

	newSt1 = logHead1.(*raftState)
	t.Log(newSt1.Msg)
	if newSt1.Msg != "Good as new" {
		t.Error("log head is not the result of a rollback")
	}

	newSt2 = logHead2.(*raftState)
	t.Log(newSt2.Msg)
	if newSt2.Msg != "Good as new" {
		t.Error("log head is not the result of a rollback")
	}
}

type badOp struct {
}

func (b badOp) ApplyTo(s consensus.State) (consensus.State, error) {
	return nil, errors.New("whops")
}

func TestBadApplyAt(t *testing.T) {
	peer1, peer2, pids := makeTwoPeers(t)
	defer peer1.Close()
	defer peer2.Close()
	raft1, opLog1, tr1 := makeTestingRaft(t, peer1, pids, &badOp{})
	defer shutdown(t, raft1)
	defer tr1.Close()
	raft2, opLog2, tr2 := makeTestingRaft(t, peer2, pids, &badOp{})
	defer shutdown(t, raft2)
	defer tr2.Close()
	defer os.RemoveAll(raftTmpFolder)

	actor1 := NewActor(raft1)
	actor2 := NewActor(raft2)
	opLog1.SetActor(actor1)
	opLog2.SetActor(actor2)

	waitForLeader(t, raft1)

	op := badOp{}
	// Only leader will succeed
	t.Log("testing CommitOp with bad operation")
	opLog1.CommitOp(op)
	opLog2.CommitOp(op)

	time.Sleep(1 * time.Second)

	if _, err := opLog1.GetLogHead(); err == nil {
		t.Fatal("expected an error")
	}

	if _, err := opLog2.GetLogHead(); err == nil {
		t.Fatal("expected an error")
	}

	// Test a ROLLBACK now
	// Only the leader will succeed
	t.Log("testing Rollback")
	opLog1.Rollback(&raftState{"Good as new"})
	opLog2.Rollback(&raftState{"Good as new"})

	time.Sleep(1 * time.Second)

	logHead1, err := opLog1.GetLogHead()
	if err != nil {
		t.Fatal(err)
	}

	logHead2, err := opLog2.GetLogHead()
	if err != nil {
		t.Fatal(err)
	}

	newSt1 := logHead1.(*raftState)
	t.Log(newSt1.Msg)
	if newSt1.Msg != "Good as new" {
		t.Error("log head is not the result of a rollback")
	}

	newSt2 := logHead2.(*raftState)
	t.Log(newSt2.Msg)
	if newSt2.Msg != "Good as new" {
		t.Error("log head is not the result of a rollback")
	}
}
