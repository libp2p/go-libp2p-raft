package libp2praft

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	consensus "github.com/libp2p/go-libp2p-consensus"
	host "github.com/libp2p/go-libp2p-host"
	peer "github.com/libp2p/go-libp2p-peer"
	peerstore "github.com/libp2p/go-libp2p-peerstore"

	"github.com/hashicorp/raft"
)

var raftTmpFolder = "testing_tmp"
var raftQuiet = true

type raftState struct {
	Msg string
}

type testOperation struct {
	Append string
}

func (o testOperation) ApplyTo(s consensus.State) (consensus.State, error) {
	raftSt := s.(*raftState)
	return &raftState{Msg: raftSt.Msg + o.Append}, nil
}

// wait 10 seconds for a leader.
func waitForLeader(t *testing.T, r *raft.Raft) {
	obsCh := make(chan raft.Observation, 1)
	observer := raft.NewObserver(obsCh, false, nil)
	r.RegisterObserver(observer)
	defer r.DeregisterObserver(observer)

	// New Raft does not allow leader observation directy
	// What's worse, there will be no notification that a new
	// leader was elected because observations are set before
	// setting the Leader and only when the RaftState has changed.
	// Therefore, we need a ticker.

	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	ticker := time.NewTicker(time.Second / 2)
	defer ticker.Stop()
	for {
		select {
		case obs := <-obsCh:
			switch obs.Data.(type) {
			case raft.RaftState:
				if r.Leader() != "" {
					return
				}
			}
		case <-ticker.C:
			if r.Leader() != "" {
				return
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for Leader")
		}
	}
}

func shutdown(t *testing.T, r *raft.Raft) {
	err := r.Shutdown().Error()
	if err != nil {
		t.Fatal(err)
	}
}

// Create a quick raft instance
func makeTestingRaft(t *testing.T, h host.Host, pids []peer.ID, op consensus.Op) (*raft.Raft, *Consensus, *raft.NetworkTransport) {
	// -- Create the consensus with no actor attached
	var consensus *Consensus
	if op != nil {
		consensus = NewOpLog(&raftState{}, op)
	} else {
		consensus = NewConsensus(&raftState{"i am not consensuated"})
	}
	// --

	// -- Create Raft servers configuration
	servers := make([]raft.Server, len(pids))
	for i, pid := range pids {
		servers[i] = raft.Server{
			Suffrage: raft.Voter,
			ID:       raft.ServerID(pid.Pretty()),
			Address:  raft.ServerAddress(pid.Pretty()),
		}
	}
	serverConfig := raft.Configuration{
		Servers: servers,
	}
	// --

	// -- Create LibP2P transports Raft
	transport, err := NewLibp2pTransport(h, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// --

	// -- Configuration
	config := raft.DefaultConfig()
	if raftQuiet {
		config.LogOutput = ioutil.Discard
		config.Logger = nil
	}
	config.LocalID = raft.ServerID(h.ID().Pretty())
	// --

	// -- SnapshotStore
	snapshots, err := raft.NewFileSnapshotStore(raftTmpFolder, 3, nil)
	if err != nil {
		t.Fatal(err)
	}

	// -- Log store and stable store: we use inmem.
	logStore := raft.NewInmemStore()
	// --

	// -- Boostrap everything if necessary
	bootstrapped, err := raft.HasExistingState(logStore, logStore, snapshots)
	if err != nil {
		t.Fatal(err)
	}

	if !bootstrapped {
		// Bootstrap cluster first
		raft.BootstrapCluster(config, logStore, logStore, snapshots, transport, serverConfig)
	} else {
		t.Log("Already initialized!!")
	}
	// --

	// Create Raft instance. Our consensus.FSM() provides raft.FSM
	// implementation
	raft, err := raft.NewRaft(config, consensus.FSM(), logStore, logStore, snapshots, transport)
	if err != nil {
		t.Fatal(err)
	}
	return raft, consensus, transport
}

func Example_consensus() {
	// This example shows how to use go-libp2p-raft to create a cluster
	// which agrees on a State. In order to do it, it defines a state,
	// creates three Raft nodes and launches them. We call a function which
	// lets the cluster leader repeteadly update the state. At the
	// end of the execution we verify that all members have agreed on the
	// same state.
	//
	// Some error handling has been excluded for simplicity

	// Declare an object which represents the State.
	// Note that State objects should have public/exported fields,
	// as they are [de]serialized.
	type raftState struct {
		Value int
	}

	// error handling ommitted
	newPeer := func(listenPort int) host.Host {
		h, _ := libp2p.New(
			context.Background(),
			libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", listenPort)),
		)
		return h
	}

	// Create peers and make sure they know about each others.
	peer1 := newPeer(9997)
	peer2 := newPeer(9998)
	peer3 := newPeer(9999)
	defer peer1.Close()
	defer peer2.Close()
	defer peer3.Close()
	peer1.Peerstore().AddAddrs(peer2.ID(), peer2.Addrs(), peerstore.PermanentAddrTTL)
	peer1.Peerstore().AddAddrs(peer3.ID(), peer3.Addrs(), peerstore.PermanentAddrTTL)
	peer2.Peerstore().AddAddrs(peer1.ID(), peer1.Addrs(), peerstore.PermanentAddrTTL)
	peer2.Peerstore().AddAddrs(peer3.ID(), peer3.Addrs(), peerstore.PermanentAddrTTL)
	peer3.Peerstore().AddAddrs(peer1.ID(), peer1.Addrs(), peerstore.PermanentAddrTTL)
	peer3.Peerstore().AddAddrs(peer2.ID(), peer2.Addrs(), peerstore.PermanentAddrTTL)

	// Create the consensus instances and initialize them with a state.
	// Note that state is just used for local initialization, and that,
	// only states submitted via CommitState() alters the state of the
	// cluster.
	consensus1 := NewConsensus(&raftState{3})
	consensus2 := NewConsensus(&raftState{3})
	consensus3 := NewConsensus(&raftState{3})

	// Create LibP2P transports Raft
	transport1, err := NewLibp2pTransport(peer1, time.Minute)
	if err != nil {
		fmt.Println(err)
		return
	}
	transport2, err := NewLibp2pTransport(peer2, time.Minute)
	if err != nil {
		fmt.Println(err)
		return
	}
	transport3, err := NewLibp2pTransport(peer3, time.Minute)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer transport1.Close()
	defer transport2.Close()
	defer transport3.Close()

	// Create Raft servers configuration for bootstrapping the cluster
	// Note that both IDs and Address are set to the Peer ID.
	servers := make([]raft.Server, 0)
	for _, h := range []host.Host{peer1, peer2, peer3} {
		servers = append(servers, raft.Server{
			Suffrage: raft.Voter,
			ID:       raft.ServerID(h.ID().Pretty()),
			Address:  raft.ServerAddress(h.ID().Pretty()),
		})
	}
	serversCfg := raft.Configuration{servers}

	// Create Raft Configs. The Local ID is the PeerOID
	config1 := raft.DefaultConfig()
	config1.LogOutput = ioutil.Discard
	config1.Logger = nil
	config1.LocalID = raft.ServerID(peer1.ID().Pretty())

	config2 := raft.DefaultConfig()
	config2.LogOutput = ioutil.Discard
	config2.Logger = nil
	config2.LocalID = raft.ServerID(peer2.ID().Pretty())

	config3 := raft.DefaultConfig()
	config3.LogOutput = ioutil.Discard
	config3.Logger = nil
	config3.LocalID = raft.ServerID(peer3.ID().Pretty())

	// Create snapshotStores
	snapshots1, err := raft.NewFileSnapshotStore("example_data1", 3, nil)
	if err != nil {
		log.Fatal("file snapshot store:", err)
	}
	snapshots2, err := raft.NewFileSnapshotStore("example_data2", 3, nil)
	if err != nil {
		log.Fatal("file snapshot store:", err)
	}
	snapshots3, err := raft.NewFileSnapshotStore("example_data3", 3, nil)
	if err != nil {
		log.Fatal("file snapshot store:", err)
	}
	defer os.RemoveAll("example_data1")
	defer os.RemoveAll("example_data2")
	defer os.RemoveAll("example_data3")

	// Create the InmemStores for use as log store and stable store.
	logStore1 := raft.NewInmemStore()
	logStore2 := raft.NewInmemStore()
	logStore3 := raft.NewInmemStore()

	// Bootsrap the stores with the serverConfigs
	raft.BootstrapCluster(config1, logStore1, logStore1, snapshots1, transport1, serversCfg.Clone())
	raft.BootstrapCluster(config2, logStore2, logStore2, snapshots2, transport2, serversCfg.Clone())
	raft.BootstrapCluster(config3, logStore3, logStore3, snapshots3, transport3, serversCfg.Clone())

	// Create Raft objects. Our consensus provides an implementation of
	// Raft.FSM
	raft1, err := raft.NewRaft(config1, consensus1.FSM(), logStore1, logStore1, snapshots1, transport1)
	if err != nil {
		log.Fatal(err)
	}
	raft2, err := raft.NewRaft(config2, consensus2.FSM(), logStore2, logStore2, snapshots2, transport2)
	if err != nil {
		log.Fatal(err)
	}
	raft3, err := raft.NewRaft(config3, consensus3.FSM(), logStore3, logStore3, snapshots3, transport3)
	if err != nil {
		log.Fatal(err)
	}

	// Create the actors using the Raft nodes
	actor1 := NewActor(raft1)
	actor2 := NewActor(raft2)
	actor3 := NewActor(raft3)

	// Set the actors so that we can CommitState() and GetCurrentState()
	consensus1.SetActor(actor1)
	consensus2.SetActor(actor2)
	consensus3.SetActor(actor3)

	// This function updates the cluster state commiting 1000 updates.
	updateState := func(c *Consensus) {
		nUpdates := 0
		for {
			if nUpdates >= 1000 {
				break
			}

			newState := &raftState{nUpdates * 2}

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
			agreedRaftState := agreedState.(*raftState)
			nUpdates++

			if nUpdates%200 == 0 {
				fmt.Printf("Performed %d updates. Current state value: %d\n",
					nUpdates, agreedRaftState.Value)
			}
		}
	}

	// Provide some time for leader election
	time.Sleep(5 * time.Second)

	// Run the 1000 updates on the leader
	// Barrier() will wait until updates have been applied
	if actor1.IsLeader() {
		updateState(consensus1)
	} else if actor2.IsLeader() {
		updateState(consensus2)
	} else if actor3.IsLeader() {
		updateState(consensus3)
	}

	// Wait for updates to arrive.
	time.Sleep(5 * time.Second)

	// Shutdown raft and wait for it to complete
	// (ignoring errors)
	raft1.Shutdown().Error()
	raft2.Shutdown().Error()
	raft3.Shutdown().Error()

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
	finalState3, err := consensus3.GetCurrentState()
	if err != nil {
		fmt.Println(err)
		return
	}
	finalRaftState1 := finalState1.(*raftState)
	finalRaftState2 := finalState2.(*raftState)
	finalRaftState3 := finalState3.(*raftState)

	fmt.Printf("Raft1 final state: %d\n", finalRaftState1.Value)
	fmt.Printf("Raft2 final state: %d\n", finalRaftState2.Value)
	fmt.Printf("Raft3 final state: %d\n", finalRaftState3.Value)
	// Output:
	// Performed 200 updates. Current state value: 398
	// Performed 400 updates. Current state value: 798
	// Performed 600 updates. Current state value: 1198
	// Performed 800 updates. Current state value: 1598
	// Performed 1000 updates. Current state value: 1998
	// Raft1 final state: 1998
	// Raft2 final state: 1998
	// Raft3 final state: 1998
}
