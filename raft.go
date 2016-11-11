// Package libp2praft implements a go-libp2p-consensus interface wrapping hashicorp/raft
// implementation and providing a libp2p network transport for it.
package libp2praft

import (
	logging "github.com/ipfs/go-log"
)

var (
	log = logging.Logger("libp2p-raft")
)
