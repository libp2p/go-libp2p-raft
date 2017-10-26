package libp2praft

import (
	"os"
	"testing"
)

func TestNewActor(t *testing.T) {
	actor := NewActor(nil)
	st, err := actor.SetState(raftState{"testing"})
	if st != nil || err == nil {
		t.Error("should fail when setting an state and raft is nil")
	}

	if actor.IsLeader() {
		t.Error("should definitely not be a leader if raft is nil")
	}

}

func TestSetState(t *testing.T) {
	peer1, peer2, pids := makeTwoPeers(t)
	defer peer1.Close()
	defer peer2.Close()
	raft1, _, tr1 := makeTestingRaft(t, peer1, pids, nil)
	defer shutdown(t, raft1)
	defer tr1.Close()
	raft2, _, tr2 := makeTestingRaft(t, peer2, pids, nil)
	defer shutdown(t, raft2)
	defer tr2.Close()
	defer os.RemoveAll(raftTmpFolder)

	actor1 := NewActor(raft1)
	actor2 := NewActor(raft2)

	waitForLeader(t, raft1)
	t.Log(actor1.Leader())
	t.Log(actor2.Leader())

	testLeader := func(actor *Actor) {
		st, err := actor.SetState(raftState{"testingLeader"})
		if err != nil {
			t.Fatal("the leader should be able to set the state")
		}

		rSt := st.(raftState)
		if rSt.Msg != "testingLeader" {
			t.Error("the returned state is not correct")
		}
	}

	testFollower := func(actor *Actor) {
		st, err := actor.SetState(raftState{"testingFollower"})
		if st != nil || err == nil {
			t.Error("the follower should not be able to set the state")
		}
	}

	if actor1.IsLeader() {
		testLeader(actor1)
	} else {
		testFollower(actor1)
	}

	if actor2.IsLeader() {
		testLeader(actor2)
	} else {
		testFollower(actor2)
	}
}

func TestActorLeader(t *testing.T) {
	peer1, peer2, pids := makeTwoPeers(t)
	defer peer1.Close()
	defer peer2.Close()
	raft1, _, tr1 := makeTestingRaft(t, peer1, pids, nil)
	defer shutdown(t, raft1)
	defer tr1.Close()
	raft2, _, tr2 := makeTestingRaft(t, peer2, pids, nil)
	defer shutdown(t, raft2)
	defer tr2.Close()
	defer os.RemoveAll(raftTmpFolder)

	actor1 := NewActor(raft1)
	waitForLeader(t, raft1)
	l, err := actor1.Leader()
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Leader is", l)
}
