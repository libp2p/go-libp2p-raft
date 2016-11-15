package libp2praft

import (
	"fmt"
	"io"
	"math/rand"
	"strings"
	"time"

	crypto "github.com/libp2p/go-libp2p-crypto"
	peer "github.com/libp2p/go-libp2p-peer"
	multiaddr "github.com/multiformats/go-multiaddr"

	multihash "github.com/multiformats/go-multihash"
)

// Peer is a container for information related to libp2p nodes so
// that they can be described consistently across libp2praft.
type Peer struct {
	ID         peer.ID
	Addrs      []multiaddr.Multiaddr
	PublicKey  crypto.PubKey  // Can leave empty
	PrivateKey crypto.PrivKey // Can leave empty
}

// NewPeer returns a pointer to a new peer.
func NewPeer(id peer.ID, addrs []multiaddr.Multiaddr, privKey crypto.PrivKey, pubKey crypto.PubKey) *Peer {
	return &Peer{
		ID:         id,
		Addrs:      addrs,
		PrivateKey: privKey,
		PublicKey:  pubKey,
	}
}

// NewPeerFromMultiaddress takes a multiaddress and creates a new
// peer.
//
// For example: with an address /ip4/1.2.3.5/tcp/2222/ipfs/ABCDE will create
// a peer with ID=ABCDE and Addrs=[/ip4/1.2.3.5/tcp/2222].
func NewPeerFromMultiaddress(addr multiaddr.Multiaddr) (*Peer, error) {
	pid, err := addr.ValueForProtocol(multiaddr.P_IPFS)
	if err != nil {
		return nil, err
	}

	strAddr := strings.Split(addr.String(), "/ipfs/")[0]
	maddr, err := multiaddr.NewMultiaddr(strAddr)
	if err != nil {
		return nil, err
	}
	peerID, err := peer.IDB58Decode(pid)
	if err != nil {
		return nil, err
	}

	return &Peer{
		ID:    peerID,
		Addrs: []multiaddr.Multiaddr{maddr},
	}, nil
}

// NewRandomPeer returns a peer which listens on the given tcp port
// on localhost. The peer ID is randomly generated and has no key pair
// attached
func NewRandomPeer(listenPort int) (*Peer, error) {
	src := rand.NewSource(time.Now().UnixNano())
	reader := rand.New(src)
	buf := make([]byte, 16)
	if _, err := io.ReadFull(reader, buf); err != nil {
		return nil, err
	}
	hash, err := multihash.Sum(buf, multihash.SHA2_256, -1)
	if err != nil {
		return nil, err
	}
	pid := peer.ID(hash)
	maddr, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", listenPort))
	if err != nil {
		return nil, err
	}
	return &Peer{
		ID:    pid,
		Addrs: []multiaddr.Multiaddr{maddr},
	}, nil
}

// Peerstore implements a simple hashicorp/Raft peerstore with some
// extra methods for working with peers of type Peer.
type Peerstore struct {
	peers []string
}

// Peers returns the peers in the Peerstore.
func (ps *Peerstore) Peers() ([]string, error) {
	return ps.peers, nil
}

// SetPeers allows to set the peers in these Peerstore.
// For passing in peers in Peer format, see AddRaftPeer and SetRaftPeers
func (ps *Peerstore) SetPeers(peers []string) error {
	ps.peers = peers
	return nil
}

// AddRaftPeer allows to add a Peer to the Peerstore
// It does not check if the peer is already in it.
func (ps *Peerstore) AddRaftPeer(p *Peer) {
	ps.peers = append(ps.peers, p.ID.Pretty())
}

// SetRaftPeers allows to set the peers of the Peerstore and
// thus, removes any previous list.
func (ps *Peerstore) SetRaftPeers(peers []*Peer) {
	ps.peers = []string{}
	for _, p := range peers {
		ps.AddRaftPeer(p)
	}
}
