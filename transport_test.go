package libp2praft

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// Most of transport is tested with the example or by just
// creating a raft instance.

func TestTransportSnapshots(t *testing.T) {
	defer os.RemoveAll(raftTmpFolder)

	peer1, peer2, pids := makeTwoPeers(t)
	defer peer1.Close()
	defer peer2.Close()

	raft1, c1, tr1 := makeTestingRaft(t, peer1, pids, nil)
	raft2, c2, tr2 := makeTestingRaft(t, peer2, pids, nil)

	actor1 := NewActor(raft1)
	actor2 := NewActor(raft2)

	c1.SetActor(actor1)
	c2.SetActor(actor2)

	waitForLeader(t, raft1)

	for i := 0; i < 5000; i++ {
		if actor1.IsLeader() {
			_, err := c1.CommitState(&raftState{fmt.Sprintf("count: %d", i)})
			if err != nil {
				t.Fatal(err)
			}
		} else if actor2.IsLeader() {
			_, err := c2.CommitState(&raftState{fmt.Sprintf("count: %d", i)})
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
	err := future.Error() //wait for snapshot
	if err != nil {
		t.Fatalf("error taking snapshot: %s", err)
	}

	raft1.Shutdown().Error()
	raft2.Shutdown().Error()
	tr1.Close()
	tr2.Close()

	t.Log("forcing Raft1 to restore the snapshot")
	raft1, c1, tr1 = makeTestingRaft(t, peer1, pids, nil)
	defer shutdown(t, raft1)
	defer tr1.Close()

	// So the new raft2 cannot load the snapshot
	raftTmpFolderOrig := raftTmpFolder
	raftTmpFolder = "testing_tmp2"
	defer os.RemoveAll("testing_tmp2")

	raft2, c2, tr2 = makeTestingRaft(t, peer1, pids, nil)
	defer shutdown(t, raft2)
	defer tr2.Close()
	time.Sleep(2 * time.Second)

	newst, err := c1.GetCurrentState()
	st := newst.(*raftState)
	if st.Msg != "count: 4999" {
		t.Error("state not restored correctly")
		t.Error(st.Msg)
	}
	raftTmpFolder = raftTmpFolderOrig
}
