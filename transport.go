package libp2praft

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	inet "github.com/libp2p/go-libp2p-net"
	peer "github.com/libp2p/go-libp2p-peer"
	peerstore "github.com/libp2p/go-libp2p-peerstore"
	protocol "github.com/libp2p/go-libp2p-protocol"
	swarm "github.com/libp2p/go-libp2p-swarm"
	bhost "github.com/libp2p/go-libp2p/p2p/host/basic"

	raft "github.com/hashicorp/raft"
	codec "github.com/ugorji/go/codec"
)

// This file contains the implementation of Hashicorp's raft Transport and
// AppendPipelines interfaces

const (
	// rpc* constants are used to identify which type of RPC we are sending
	// on the cable so we can parse it back into the right type
	// see streamHandler() and "rpcType" variables.
	rpcAppendEntries uint8 = iota
	rpcRequestVote
	rpcInstallSnapshot

	// RaftProtocol is the name of the protocol used for handling
	// single RPC commands
	RaftProtocol protocol.ID = "/raft/0.0.1/rpc"
	// RaftPipelineProtocol is the name of the protocol used for
	// handling Raft pipilined requests
	RaftPipelineProtocol protocol.ID = "/raft/0.0.1/pipeline"

	rpcMaxPipeline = 256
)

var (
	// ErrTransportShutdown is returned when operations on a transport are
	// invoked after it's been terminated.
	ErrTransportShutdown = errors.New("transport shutdown")

	// ErrPipelineShutdown is returned when the pipeline is closed.
	ErrPipelineShutdown = errors.New("append pipeline closed")
)

// Libp2pTransport implements the hashicorp/raft Transport interface
// and allows using LibP2P as the transport. The implementation is mostly
// inspired in the existing hashicorp/raft.NetworkTransport.
//
// The main difference to the user is that, instead of identifying Raft peers
// by an IP/port pair, we identify them by Peer ID and multiaddresses. Peers
// maintain an address which provides multiaddresses for every peer ID,
// thus allowing peers to communicate with eachothers over the multiple
// transports potentially supported by LibP2P.
type Libp2pTransport struct {
	host *bhost.BasicHost
	ctx  context.Context

	consumeCh chan raft.RPC

	heartbeatFn     func(raft.RPC)
	heartbeatFnLock sync.Mutex

	shutdown     bool
	shutdownCh   chan struct{}
	shutdownLock sync.Mutex

	//timeout      time.Duration
	//TimeoutScale int
}

// streamWrap wraps a libp2p stream. We encode/decode whenever we
// write/read from a stream, so we can just carry the encoders
// and bufios with us
type streamWrap struct {
	stream inet.Stream
	enc    *codec.Encoder
	dec    *codec.Decoder
	w      *bufio.Writer
	r      *bufio.Reader
}

// wrapStream takes a stream and complements it with r/w bufios and
// decoder/encoder. In order to write to the stream we can use
// wrap.w.Write(). To encode something into it we can wrap.enc.Encode().
// Finally, we should wrap.w.Flush() to actually send the data. Similar
// for receiving.
func wrapStream(s inet.Stream) *streamWrap {
	reader := bufio.NewReader(s)
	writer := bufio.NewWriter(s)
	dec := codec.NewDecoder(reader, &codec.MsgpackHandle{})
	enc := codec.NewEncoder(writer, &codec.MsgpackHandle{})
	return &streamWrap{
		stream: s,
		r:      reader,
		w:      writer,
		enc:    enc,
		dec:    dec,
	}
}

// NewLibp2pTransport returns a new Libp2pTransport. The localPeer addresses
// are used for listening to incoming connections. The clusterPeers are
// addresses are added to the localPeer address book, so they can be dialed
// in the future. The connection on which the streams are multiplexed is only
// opened on demand.
//
// Libp2pTransport streams are opened and closed for each operation. However,
// the Transport also offers the way for Raft to open a permanent stream
// (AppendPipeline). This ensures a connection (and a stream) will stay open
// between peers and reduces the cost of opening/closing streams.
func NewLibp2pTransport(localPeer *Peer, clusterPeers []*Peer) (*Libp2pTransport, error) {
	// TODO(hector): Connect to peers now to speed up startup and arise errors earlier.
	ctx := context.Background()
	ps := peerstore.NewPeerstore()
	if localPeer.PublicKey != nil && localPeer.PrivateKey != nil {
		// Enable secio
		err := ps.AddPubKey(localPeer.ID, localPeer.PublicKey)
		if err != nil {
			return nil, err
		}
		err = ps.AddPrivKey(localPeer.ID, localPeer.PrivateKey)
		if err != nil {
			return nil, err
		}
	}

	network, err := swarm.NewNetwork(
		ctx,
		localPeer.Addrs,
		localPeer.ID,
		ps,
		nil)

	if err != nil {
		return nil, err
	}

	host := bhost.New(network)

	t := &Libp2pTransport{
		host:       host,
		ctx:        ctx,
		consumeCh:  make(chan raft.RPC),
		shutdownCh: make(chan struct{}),
	}

	if err := t.AddPeers(clusterPeers); err != nil {
		return nil, err
	}

	host.SetStreamHandler(RaftProtocol, func(s inet.Stream) {
		wrap := wrapStream(s)
		defer s.Close()
		err := t.streamHandler(wrap)
		if err != nil {
			logger.Errorf("%s: %s", t.LocalAddr(), err)
		}

	})

	// The difference is that these are long lived
	host.SetStreamHandler(RaftPipelineProtocol, func(s inet.Stream) {
		wrap := wrapStream(s)
		for {
			err := t.streamHandler(wrap)
			if err == io.EOF {
				logger.Errorf("%s: EOF while handling pipeline stream", t.LocalAddr())
				s.Close()
				break
			}
			if err != nil {
				logger.Errorf("%s: %s", t.LocalAddr(), err)
				break
			}
		}
	})

	return t, nil
}

// streamHandler does most of the heavy job when handling a stream: place the
// RPC in the consumeCh for Raft and send a response.
// It is mostly copied from NetworkTransport.handleCommand()
func (t *Libp2pTransport) streamHandler(wrap *streamWrap) error {
	logger.Debugf("%s: Handling stream", t.LocalAddr())

	// Read rpcType
	rpcType, err := wrap.r.ReadByte()
	if err != nil {
		return err
	}

	// Preare the RPC object for Raft consumption
	// This is the channel that we use in case Raft wants to
	// send a response
	respCh := make(chan raft.RPCResponse, 1)
	rpc := raft.RPC{
		RespChan: respCh,
	}

	// We shortcut Raft for heartbeats
	isHeartbeat := false
	switch rpcType {
	case rpcAppendEntries:
		logger.Debugf("%s: An append entries request has arrived", t.LocalAddr())
		var req raft.AppendEntriesRequest
		err = wrap.dec.Decode(&req)
		if err != nil {
			return err
		}

		rpc.Command = &req

		// Check if this is a heartbeat
		if req.Term != 0 && req.Leader != nil &&
			req.PrevLogEntry == 0 && req.PrevLogTerm == 0 &&
			len(req.Entries) == 0 && req.LeaderCommitIndex == 0 {
			isHeartbeat = true
		}

	case rpcRequestVote:
		logger.Debugf("%s: A request vote request has arrived", t.LocalAddr())
		var req raft.RequestVoteRequest
		err = wrap.dec.Decode(&req)
		if err != nil {
			return err
		}
		rpc.Command = &req

	case rpcInstallSnapshot:
		logger.Debugf("%s: An install snapshot request has arrived", t.LocalAddr())
		// for InstallSnapshot, send: RPC\EOF\data\EOF
		var req raft.InstallSnapshotRequest
		err = wrap.dec.Decode(&req)
		if err != nil {
			return err
		}
		rpc.Command = &req
		rpc.Reader = io.LimitReader(wrap.r, req.Size)

	default:
		return fmt.Errorf("unknown rpc type: %d", rpcType)
	}

	if isHeartbeat {
		t.heartbeatFnLock.Lock()
		fn := t.heartbeatFn
		t.heartbeatFnLock.Unlock()
		if fn != nil {
			fn(rpc)
			goto RESP
		}
	}

	// Now that we have the RPC sent by the other node
	// Leave it ready for consumption by Raft in the consume channel
	select {
	case t.consumeCh <- rpc: // Put the rpc in the consumeCh for Raft
	case <-t.shutdownCh:
		return ErrTransportShutdown
	}

RESP: // When Raft is done, it informs us via respCh and we can send a response
	select {
	case resp := <-respCh:
		// Send the error first
		// Not clear why it is done this way though rather than
		// sending the whole resp.Response altogether (Hector)
		respErr := ""
		if resp.Error != nil {
			respErr = resp.Error.Error()
		}
		if err := wrap.enc.Encode(respErr); err != nil {
			return err
		}

		// Send the response
		if err := wrap.enc.Encode(resp.Response); err != nil {
			return err
		}

		// Flush the response to the stream
		if err := wrap.w.Flush(); err != nil {
			return err
		}
		logger.Debugf("%s: Response sent", t.LocalAddr())

	case <-t.shutdownCh:
		return ErrTransportShutdown
	}

	return nil
}

// makeRPC makes a generic RPC Request on a new stream, reads a response
// and closes it
func (t *Libp2pTransport) makeRPC(ctx context.Context, target string, rpcType uint8, args interface{}, resp interface{}) error {
	// Get a stream to the target
	p, err := peer.IDB58Decode(target)
	if err != nil {
		return err
	}
	stream, err := t.host.NewStream(t.ctx, p, RaftProtocol)
	if err != nil {
		return err
	}
	defer stream.Close()

	wrap := wrapStream(stream)

	// Send it
	err = sendRPC(wrap, rpcType, args)
	if err != nil {
		return err
	}

	// Check response
	if err := receiveRPCResponse(wrap, resp); err != nil {
		return err
	}
	return nil
}

// sendRPC sends a generic RPC Request.
func sendRPC(wrap *streamWrap, rpcType uint8, args interface{}) error {
	// Write the request type to the stream first
	err := wrap.w.WriteByte(byte(rpcType))
	if err != nil {
		return err
	}

	// Then write the request
	if err := wrap.enc.Encode(args); err != nil {
		return err
	}

	// Flush it to the stream
	if err := wrap.w.Flush(); err != nil {
		return err
	}
	return nil
}

// receiveRPCResponse receives a generic RPC Response (see streamHandler())
// for details about how this response is constructed.
func receiveRPCResponse(wrap *streamWrap, rpcResp interface{}) error {
	logger.Debugf("%s: Receiving RPC Response", wrap.stream.Conn().LocalPeer().Pretty())

	// Check for error first
	var rpcError string
	err := wrap.dec.Decode(&rpcError)

	if err == io.EOF { // The stream was closed
		return err
	}

	if err != nil {
		return errors.New(fmt.Sprint("error decoding rpcError: ", err))
	}

	// Decode the response
	if err := wrap.dec.Decode(rpcResp); err != nil {
		return errors.New(fmt.Sprint("error decoding rpcResponse: ", err))
	}

	logger.Debugf("%s: Valid response received", wrap.stream.Conn().LocalPeer().Pretty())

	// Format an error if any
	if rpcError != "" {
		return fmt.Errorf(rpcError)
	}
	return nil
}

// Consumer returns a channel that can be used to
// consume RPC requests by the rest of Raft.
func (t *Libp2pTransport) Consumer() <-chan raft.RPC {
	return t.consumeCh
}

// LocalAddr is used to return our local address to distinguish from our peers.
// In our case it is just our Peer ID. Other nodes can always talk to this
// peer ID because they have the associated multiaddresses in the address book.
func (t *Libp2pTransport) LocalAddr() string {
	return peer.IDB58Encode(t.host.ID())
}

// // LocalMAddrs returns the Multiaddresses this node is listening on in string
// // form. Example: /ip4/127.0.0.1/ipfs/nodeID
// func (t *Libp2pTransport) LocalMAddrs() []string {
// 	hostAddrs := t.host.Addrs()
// 	var addrs []string
// 	for _, addr := range hostAddrs {
// 		addrs = append(addrs,
// 			fmt.Sprintf("%s/ipfs/%s", addr, t.LocalAddr()))
// 	}
// 	return addrs
// }

// AddPeers adds new peers to the address book, thus allowing
// to connect to them via their multiaddresses. This is useful when
// adding new members to the Raft cluster.
func (t *Libp2pTransport) AddPeers(peers []*Peer) error {
	for _, p := range peers {
		if err := t.AddPeer(p); err != nil {
			return err
		}
	}
	return nil
}

// AddPeer adds a new peer to the address book, thus allowing
// to connect to it via its multiaddresses. This is useful when
// adding new members to the Raft cluster.
func (t *Libp2pTransport) AddPeer(peer *Peer) error {
	t.host.Peerstore().AddAddrs(
		peer.ID,
		peer.Addrs,
		peerstore.PermanentAddrTTL)
	return nil
}

// AppendEntriesPipeline returns an interface that can be used to pipeline
// AppendEntries requests. This interface maintains an open stream to the target
// peer and speeds up appending entries to the Raft log.
func (t *Libp2pTransport) AppendEntriesPipeline(target string) (raft.AppendPipeline, error) {
	pipeline, err := newLibp2pPipeline(t, target)
	if err != nil {
		return nil, err
	}
	return pipeline, nil
}

// AppendEntries sends the appropriate RPC to the target node.
func (t *Libp2pTransport) AppendEntries(target string, args *raft.AppendEntriesRequest, resp *raft.AppendEntriesResponse) error {
	err := t.makeRPC(t.ctx, target, rpcAppendEntries, args, resp)
	if err != nil {
		return err
	}
	return nil
}

// RequestVote sends the appropriate RPC to the target node.
func (t *Libp2pTransport) RequestVote(target string, args *raft.RequestVoteRequest, resp *raft.RequestVoteResponse) error {
	err := t.makeRPC(t.ctx, target, rpcRequestVote, args, resp)
	if err != nil {
		return err
	}
	return nil
}

// InstallSnapshot is used to push a snapshot down to a follower. The data is read from
// the ReadCloser and streamed to the client.
func (t *Libp2pTransport) InstallSnapshot(target string, args *raft.InstallSnapshotRequest, resp *raft.InstallSnapshotResponse, data io.Reader) error {
	logger.Debug("%s: Installing snapshot", t.LocalAddr())

	// Install snapshot is special because we need to stream data so
	// we cannot re-use our makeRPC function. We go manual

	// Get a stream to the target
	p, err := peer.IDB58Decode(target)
	if err != nil {
		return err
	}
	stream, err := t.host.NewStream(t.ctx, p, RaftProtocol)
	defer stream.Close()
	if err != nil {
		return err
	}

	wrap := wrapStream(stream)

	// First we send the RPC
	err = sendRPC(wrap, rpcInstallSnapshot, args)
	if err != nil {
		return err
	}

	// Then we stream the data
	_, err = io.Copy(wrap.w, data)
	if err != nil {
		return err
	}

	// And make sure it is flushed
	if err := wrap.w.Flush(); err != nil {
		return err
	}

	// Get the response and return it
	if err := receiveRPCResponse(wrap, resp); err != nil {
		return err
	}
	return nil
}

// EncodePeer is used to serialize a peer name.
func (t *Libp2pTransport) EncodePeer(p string) []byte {
	return []byte(p)
}

// DecodePeer is used to deserialize a peer name.
func (t *Libp2pTransport) DecodePeer(p []byte) string {
	return string(p)
}

// SetHeartbeatHandler is used to setup a heartbeat handler
// as a fast-pass. This is to avoid head-of-line blocking from
// disk IO. If a Transport does not support this, it can simply
// ignore the call, and push the heartbeat onto the Consumer channel.
func (t *Libp2pTransport) SetHeartbeatHandler(cb func(rpc raft.RPC)) {
	// The bypassing happens in streamHandler() when we detec a request
	// is a heartbeat.
	t.heartbeatFnLock.Lock()
	defer t.heartbeatFnLock.Unlock()
	t.heartbeatFn = cb
}

// Close terminates a Libp2p transport.
func (t *Libp2pTransport) Close() error {
	t.shutdownLock.Lock()
	defer t.shutdownLock.Unlock()

	if !t.shutdown {
		close(t.shutdownCh)
		t.shutdown = true
	}
	return nil
}

// OpenConns opens connections to all known peers. It is
// not necessary to use it, as connections are opened on
// demand when creating streams, but is useful to warm
// up the transports before letting raft use them and checking
// if LibP2P has connectivity to other peers
func (t *Libp2pTransport) OpenConns() error {
	peers := t.host.Peerstore().Peers()
	for _, p := range peers {
		peerInfo := t.host.Peerstore().PeerInfo(p)
		if err := t.host.Connect(t.ctx, peerInfo); err != nil {
			logger.Errorf("%s: Error opening connection: %s", t.LocalAddr(), err)
			return err
		}
	}
	return nil
}

// libp2pPipeline implements raft.AppendPipeline.
// It is used for pipelining AppendEntries requests. It is used to increase the
// replication throughput by masking latency and better utilizing bandwidth.
//
// A pipeline works by basically allowing to send whatever
// AppendEntriesRequest we have on a stream and have on the
// side a routine for processing responses from it, thus
// decoupling the send-then-receive operation and allowing multiple
// requests to stand in queue.
type libp2pPipeline struct {
	trans *Libp2pTransport
	sWrap *streamWrap

	// This is how the other implementations do it
	doneCh       chan raft.AppendFuture
	inProgressCh chan *appendFuture

	shutdown     bool
	shutdownCh   chan struct{}
	shutdownLock sync.Mutex
}

// appendFuture implements raft.AppendFuture.
// It is used for waiting on a pipelined append
// entries RPC.
// Copied from raft future.go because it is private there
// even though I don't know why
type appendFuture struct {
	deferError
	start time.Time
	args  *raft.AppendEntriesRequest
	resp  *raft.AppendEntriesResponse
}

func (a *appendFuture) Start() time.Time {
	return a.start
}

func (a *appendFuture) Request() *raft.AppendEntriesRequest {
	return a.args
}

func (a *appendFuture) Response() *raft.AppendEntriesResponse {
	return a.resp
}

// deferError can be embedded to allow a future
// to provide an error in the future.
// Copied from raft future.go
type deferError struct {
	err       error
	errCh     chan error
	responded bool
}

func (d *deferError) init() {
	d.errCh = make(chan error, 1)
}

func (d *deferError) Error() error {
	if d.err != nil {
		// Note that when we've received a nil error, this
		// won't trigger, but the channel is closed after
		// send so we'll still return nil below.
		return d.err
	}
	if d.errCh == nil {
		panic("waiting for response on nil channel")
	}
	d.err = <-d.errCh
	return d.err
}

func (d *deferError) respond(err error) {
	if d.errCh == nil {
		return
	}
	if d.responded {
		return
	}
	d.errCh <- err
	close(d.errCh)
	d.responded = true
}

// newLibp2pPipeline opens a stream to the target, launches a go routine
// to read responses from that stream continuosly
func newLibp2pPipeline(tr *Libp2pTransport, target string) (*libp2pPipeline, error) {
	logger.Debugf("%s: New pipeline to %s", tr.LocalAddr(), target)
	p, err := peer.IDB58Decode(target)
	if err != nil {
		return nil, err
	}

	stream, err := tr.host.NewStream(tr.ctx, p, RaftPipelineProtocol)
	if err != nil {
		logger.Debugf("%s: %s", tr.LocalAddr(), err)
		return nil, err
	}
	wrap := wrapStream(stream)

	pipeline := &libp2pPipeline{
		trans: tr,
		sWrap: wrap,

		doneCh:       make(chan raft.AppendFuture, rpcMaxPipeline),
		inProgressCh: make(chan *appendFuture, rpcMaxPipeline),
		shutdownCh:   make(chan struct{}),
	}

	go pipeline.decodeResponses()
	return pipeline, nil
}

// decodeResponses is a long running routine that decodes the responses
// sent on the stream of a pipeline. It is informed it has to read a response
// by a "future" appearing on the inProgressCh. When the response is read,
// the future is moved to the doneCh, which is consumed by Raft.
func (p *libp2pPipeline) decodeResponses() {
	for {
		select {
		case future := <-p.inProgressCh:
			err := receiveRPCResponse(p.sWrap, future.resp)
			if err != nil {
				logger.Errorf("%s: Pipeline error: %s", p.trans.LocalAddr(), err)
				p.Close() // We abort this pipeline
			}
			future.respond(err)

			select {
			case p.doneCh <- future:
			case <-p.shutdownCh: // kill goroutine
				return
			}
		case <-p.shutdownCh:
			return // kill goroutine
		}

	}
}

// AppendEntries is used to add another request to the pipeline.
func (p *libp2pPipeline) AppendEntries(args *raft.AppendEntriesRequest, resp *raft.AppendEntriesResponse) (raft.AppendFuture, error) {
	// Create a new future
	future := &appendFuture{
		start: time.Now(),
		args:  args,
		resp:  resp,
	}
	future.init()

	// Send this request
	logger.Debugf("%s: Appending request to pipeline", p.trans.LocalAddr())
	err := sendRPC(p.sWrap, rpcAppendEntries, args)
	if err != nil {
		return nil, err
	}

	// Put it in line for processing the response
	select {
	case p.inProgressCh <- future:
		return future, nil
	case <-p.shutdownCh:
		return nil, ErrPipelineShutdown
	}
}

// Consumer returns a channel that can be used to consume
// response futures when they are ready.
func (p *libp2pPipeline) Consumer() <-chan raft.AppendFuture {
	// Raft can consume from the requests that have received response
	return p.doneCh
}

// Close closes the pipeline and cancels all inflight RPCs
func (p *libp2pPipeline) Close() error {
	logger.Infof("%s: Shutting down pipeline", p.trans.LocalAddr())
	p.shutdownLock.Lock()
	defer p.shutdownLock.Unlock()

	if p.shutdown {
		return nil
	}

	err := p.sWrap.stream.Close()
	if err != nil {
		return err
	}

	p.shutdown = true
	close(p.shutdownCh)
	return nil
}
