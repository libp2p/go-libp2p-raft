package libp2praft

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestTransportSnapshots(t *testing.T) {
	// Most of transport is tested with the example or by just
	// creating a raft instance.
	defer os.RemoveAll(raftTmpFolder)

	peer1, _ := NewRandomPeer(9997)
	peer2, _ := NewRandomPeer(9998)
	peers1 := []*Peer{peer2}
	peers2 := []*Peer{peer1}

	raft1, c1, tr1, err := makeTestingRaft(peer1, peers1, nil)
	if err != nil {
		t.Fatal(err)
	}
	raft2, c2, tr2, err := makeTestingRaft(peer2, peers2, nil)
	if err != nil {
		t.Fatal(err)
	}

	actor1 := NewActor(raft1)
	actor2 := NewActor(raft2)

	c1.SetActor(actor1)
	c2.SetActor(actor2)

	time.Sleep(2 * time.Second)

	for i := 0; i < 5000; i++ {
		if actor1.IsLeader() {
			_, err := c1.CommitState(raftState{fmt.Sprintf("count: %d", i)})
			if err != nil {
				t.Fatal(err)
			}
		} else if actor2.IsLeader() {
			_, err := c2.CommitState(raftState{fmt.Sprintf("count: %d", i)})
			if err != nil {
				t.Fatal(err)
			}
		} else {
			t.Fatal("no leaders")
		}
	}

	time.Sleep(2 * time.Second)

	t.Log("forcing Raft1 to take a snapshot")
	// Force raft to take a snapshot
	future := raft1.Snapshot()
	err = future.Error() //wait for snapshot
	if err != nil {
		t.Fatalf("error taking snapshot: %s", err)
	}

	raft1.Shutdown()
	raft2.Shutdown()
	tr1.Close()
	tr2.Close()

	t.Log("forcing Raft1 to restore the snapshot")
	raft1, c1, tr1, err = makeTestingRaft(peer1, peers1, nil)
	if err != nil {
		t.Fatalf("raft1: %s", err)
	}
	defer raft1.Shutdown()
	defer tr1.Close()

	// So the new raft2 cannot load the snapshot
	raftTmpFolderOrig := raftTmpFolder
	raftTmpFolder = "testing_tmp2"
	defer os.RemoveAll("testing_tmp2")

	raft2, c2, tr2, err = makeTestingRaft(peer2, peers2, nil)
	if err != nil {
		t.Fatalf("raft2: %s", err)
	}
	defer raft2.Shutdown()
	defer tr2.Close()
	time.Sleep(2 * time.Second)

	newst, err := c1.GetCurrentState()
	st := newst.(raftState)
	if st.Msg != "count: 4999" {
		t.Error("state not restored correctly")
	}
	raftTmpFolder = raftTmpFolderOrig
}

func TestNewLibp2pTransportWithHost(t *testing.T) {
	defer os.RemoveAll(raftTmpFolder)

	peer1, _ := NewRandomPeer(9997)
	peer2, _ := NewRandomPeer(9998)
	peers1 := []*Peer{peer2}
	peers2 := []*Peer{peer1}

	raft1, _, tr1, err := makeTestingRaft(peer1, peers1, nil)
	if err != nil {
		t.Fatal(err)
	}

	raft2, _, tr2, err := makeTestingRaft(peer2, peers2, nil)
	if err != nil {
		t.Fatal(err)
	}

	raft1.Shutdown()
	raft2.Shutdown()

	trWithHost1, err1 := NewLibp2pTransportWithHost(tr1.host)
	trWithHost2, err2 := NewLibp2pTransportWithHost(tr2.host)

	defer tr1.Close() // This will shutdown the host
	defer tr2.Close()
	defer trWithHost1.Close() // This shutsdown transport but not host
	defer trWithHost2.Close()

	if err1 != nil || err2 != nil {
		t.Error(err1)
		t.Error(err2)
		t.FailNow()
	}

	if err := trWithHost1.OpenConns(); err != nil {
		t.Fatal(err)
	}

	if err := trWithHost2.OpenConns(); err != nil {
		t.Fatal(err)
	}

	if trWithHost1.Host().ID() != peer1.ID || trWithHost2.Host().ID() != peer2.ID {
		t.Error("host IDs should match")
	}
}
