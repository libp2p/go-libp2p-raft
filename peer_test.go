package libp2praft

import (
	"fmt"
	"testing"

	multiaddr "github.com/multiformats/go-multiaddr"
)

func TestNewPeer(t *testing.T) {
	rPeer, err := NewRandomPeer(1000)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := multiaddr.NewMultiaddr("/ip4/0.0.0.0/tcp/1000")
	if err != nil {
		t.Fatal(err)
	}

	p := NewPeer(rPeer.ID, []multiaddr.Multiaddr{addr}, nil, nil)
	if p.ID != rPeer.ID {
		t.Error("IDs should be the same")
	}
}

func TestNewPeerFromMultiaddress(t *testing.T) {
	rPeer, err := NewRandomPeer(2000)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/1000/ipfs/%s", rPeer.ID.Pretty()))
	if err != nil {
		t.Fatal(err)
	}

	peer, err := NewPeerFromMultiaddress(addr)
	if err != nil {
		t.Fatal(err)
	}
	if peer.ID != rPeer.ID {
		t.Error("IDs should be the same")
	}

	addr2, err := multiaddr.NewMultiaddr("/ip4/0.0.0.0/tcp/1000")
	if err != nil {
		t.Fatal(err)
	}
	if len(peer.Addrs) != 1 {
		t.Fatal("Peer should have 1 address")
	}
	if !peer.Addrs[0].Equal(addr2) {
		t.Errorf("Wrong peer address: %s != %s", peer.Addrs[0], addr2)
	}
}
