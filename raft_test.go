// Package libp2praft implements a go-libp2p-consensus interface wrapping hashicorp/raft
// implementation and providing a libp2p network transport for it.
package libp2praft

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb"
)

func Example_consensus() {
	// This example shows how to use go-libp2p-raft to create a cluster
	// which agrees on a State. In order to do it, it defines a state,
	// creates three Raft nodes and launches them. A subroutine
	// then lets the cluster leader repeteadly update the state. At the
	// end of the execution we verify that all members have agreed on the
	// same state.

	raftTmpFolder := "raft_example_tmp" // deleted at the end

	// Declare an object which represents the State.
	type raftState struct {
		Value int
	}

	// Create peers and add them to Raft peerstores
	peer1, _ := NewRandomPeer(9998)
	peer2, _ := NewRandomPeer(9999)
	peers1 := []*Peer{peer2}
	peers2 := []*Peer{peer1}
	pstore1 := &Peerstore{}
	pstore2 := &Peerstore{}
	pstore1.SetRaftPeers(peers1)
	pstore2.SetRaftPeers(peers2)

	// Create LibP2P transports Raft
	transport1, err := NewLibp2pTransport(peer1, peers1)
	if err != nil {
		fmt.Println(err)
		return
	}
	transport2, err := NewLibp2pTransport(peer2, peers2)
	if err != nil {
		fmt.Println(err)
		return
	}

	transport1.OpenConns()
	transport2.OpenConns()

	// Create the consensus instances and initialize them with a state.
	// Note that state is just used for local initialization, and that,
	// only states submitted via CommitState() alters the state of the
	// cluster.
	consensus1 := NewConsensus(raftState{3})
	consensus2 := NewConsensus(raftState{3})

	// Hashicorp/raft initialization
	config := raft.DefaultConfig()
	config.LogOutput = nil
	config.Logger = nil
	// SnapshotStore
	snapshots1, err := raft.NewFileSnapshotStore(raftTmpFolder, 3, os.Stderr)
	if err != nil {
		log.Fatal("file snapshot store:", err)
	}
	snapshots2, err := raft.NewFileSnapshotStore(raftTmpFolder, 3, os.Stderr)
	if err != nil {
		log.Fatal("file snapshot store:", err)
	}
	// Create the log store and stable store.
	logStore1, err := raftboltdb.NewBoltStore(raftTmpFolder + "/raft1.db")
	if err != nil {
		log.Fatal("new bolt store:", err)
	}
	// Create the log store and stable store.
	logStore2, err := raftboltdb.NewBoltStore(raftTmpFolder + "/raft2.db")
	if err != nil {
		log.Fatal("new bolt store:", err)
	}

	// Raft node creation: we use our consensus objects directly as they
	// implement Raft FSM interface.
	raft1, err := raft.NewRaft(config, consensus1, logStore1, logStore1, snapshots1, pstore1, transport1)
	raft2, err := raft.NewRaft(config, consensus2, logStore2, logStore2, snapshots2, pstore2, transport2)

	// We create the actors using the Raft nodes
	actor1 := NewActor(raft1)
	actor2 := NewActor(raft2)

	// We set the actors so that we can CommitState() and GetCurrentState()
	consensus1.SetActor(actor1)
	consensus2.SetActor(actor2)

	// This function updates the cluster state commiting 1000 updates.
	updateState := func(c *Consensus) {
		nUpdates := 0
		for {
			if nUpdates >= 1000 {
				break
			}

			newState := raftState{nUpdates * 2}

			// CommitState() blocks until the state has been
			// agreed upon by everyone
			agreedState, err := c.CommitState(newState)
			if err != nil {
				fmt.Println(err)
				continue
			}
			if agreedState == nil {
				fmt.Println("agreedState is nil: commited on a non-leader?")
				continue
			}
			agreedRaftState := agreedState.(raftState)
			nUpdates++

			if nUpdates%200 == 0 {
				fmt.Printf("Performed %d updates. Current state value: %d\n",
					nUpdates, agreedRaftState.Value)
			}
		}
	}

	// Provide some time for leader election
	time.Sleep(2 * time.Second)

	// Run the 1000 updates on the leader
	if actor1.IsLeader() {
		updateState(consensus1)
	} else if actor2.IsLeader() {
		updateState(consensus2)
	}

	// Provide some time for all nodes to catch up
	time.Sleep(2 * time.Second)
	// Shutdown raft and wait for it to complete
	// (ignoring errors)
	raft1.Shutdown().Error()
	raft1.Shutdown().Error()
	os.RemoveAll(raftTmpFolder)

	// Final states
	finalState1, err := consensus1.GetCurrentState()
	if err != nil {
		fmt.Println(err)
		return
	}
	finalState2, err := consensus2.GetCurrentState()
	if err != nil {
		fmt.Println(err)
		return
	}
	finalRaftState1 := finalState1.(raftState)
	finalRaftState2 := finalState2.(raftState)

	fmt.Printf("Raft1 final state: %d\n", finalRaftState1.Value)
	fmt.Printf("Raft2 final state: %d\n", finalRaftState2.Value)
	// Output:
	// Performed 200 updates. Current state value: 398
	// Performed 400 updates. Current state value: 798
	// Performed 600 updates. Current state value: 1198
	// Performed 800 updates. Current state value: 1598
	// Performed 1000 updates. Current state value: 1998
	// Raft1 final state: 1998
	// Raft2 final state: 1998
}
