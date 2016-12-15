package libp2praft

import (
	"os"
	"testing"
	"time"
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
	peer1, _ := NewRandomPeer(9997)
	peer2, _ := NewRandomPeer(9998)
	peers1 := []*Peer{peer2}
	peers2 := []*Peer{peer1}

	raft1, _, tr1, err := makeTestingRaft(peer1, peers1, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer raft1.Shutdown()
	defer tr1.Close()
	raft2, _, tr2, err := makeTestingRaft(peer2, peers2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer raft2.Shutdown()
	defer tr2.Close()
	defer os.RemoveAll(raftTmpFolder)

	actor1 := NewActor(raft1)
	actor2 := NewActor(raft2)

	time.Sleep(3 * time.Second)

	if !actor1.IsLeader() && !actor2.IsLeader() {
		t.Fatal("raft failed to declare a leader")
	}

	t.Log(actor1.Leader())
	t.Log(actor2.Leader())

	testLeader := func(actor *Actor) {
		st, err := actor.SetState(raftState{"testingLeader"})
		if err != nil {
			t.Error("the leader should be able to set the state")
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
	peer1, _ := NewRandomPeer(9997)
	peer2, _ := NewRandomPeer(9998)
	peers1 := []*Peer{peer2}
	peers2 := []*Peer{peer1}

	raft1, _, tr1, err := makeTestingRaft(peer1, peers1, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer raft1.Shutdown()
	defer tr1.Close()
	raft2, _, tr2, err := makeTestingRaft(peer2, peers2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer raft2.Shutdown()
	defer tr2.Close()
	defer os.RemoveAll(raftTmpFolder)

	actor1 := NewActor(raft1)
	i := 0
	for i = 0; i < 20; i++ {
		l, err := actor1.Leader()
		if err != nil {
			t.Log("No leader yet")
			time.Sleep(250 * time.Millisecond)
			continue
		}
		t.Log("Leader is", l)
		break
	}
	if i == 20 {
		t.Error("Failed to declare a leader")
	}
}
