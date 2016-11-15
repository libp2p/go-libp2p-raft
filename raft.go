// Package libp2praft implements a go-libp2p-consensus interface wrapping hashicorp/raft
// implementation and providing a libp2p network transport for it.
package libp2praft

import (
	logging "gx/ipfs/QmSpJByNKFX1sCsHBEp3R73FL4NF6FnQTEGyNAXHm2GS52/go-log"
)

var (
	logger = logging.Logger("libp2p-raft")
)
