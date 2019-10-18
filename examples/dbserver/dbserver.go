package main

// This provides a global database using Raft with a libp2p-transport with a
// simple HTTP API to insert, delete, get and list items. It uses an in-memory
// database backend. Peer IDs are generated on every run.

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/raft"
	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/query"
	"github.com/ipfs/go-datastore/sync"
	logging "github.com/ipfs/go-log"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-connmgr"
	"github.com/libp2p/go-libp2p-consensus"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	libp2praft "github.com/libp2p/go-libp2p-raft"

	"github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multiaddr-net"
)

var (
	logger    = logging.Logger("raftdb")
	listen, _ = multiaddr.NewMultiaddr("/ip4/0.0.0.0/tcp/0")
	httpPort  = 0
)

func init() {
	port := os.Getenv("PORT")
	if port != "" {
		listen, _ = multiaddr.NewMultiaddr("/ip4/0.0.0.0/tcp/" + port)
		nPort, err := strconv.Atoi(port)
		if err != nil {
			return
		}
		httpPort = nPort + 1
	}
}

type Op struct {
	Key    string
	Value  string
	Method string
}

var tStart time.Time

func (op *Op) ApplyTo(st consensus.State) (consensus.State, error) {
	dstore, ok := st.(datastore.Datastore)
	if !ok {
		return nil, errors.New("unexpected state type")
	}

	switch op.Method {
	case "PUT":
		if tStart.IsZero() {
			tStart = time.Now()
		}
		k := datastore.NewKey(op.Key)
		err := dstore.Put(k, []byte(op.Value))
		if err != nil {
			return nil, err
		}
		fmt.Printf("Added: [%s] -> %s\n", k, op.Value)
		if k == datastore.NewKey("time") {
			fmt.Println(time.Since(tStart))
			tStart = time.Now()
		}
		return dstore, nil
	case "DELETE":
		k := datastore.NewKey(op.Key)
		err := dstore.Delete(k)
		if err != nil {
			return nil, err
		}
		fmt.Printf("Removed: [%s]\n", k)
		return dstore, nil
	default:
		return nil, errors.New("bad method")
	}
}

func main() {
	logging.SetLogLevel("*", "warn")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// State
	mapds := datastore.NewMapDatastore()
	store := sync.MutexWrap(mapds)
	defer store.Close()

	// Op
	op := &Op{}

	opLog := libp2praft.NewOpLog(mapds, op)

	connman := connmgr.NewConnManager(5000, 5000, time.Minute)

	opts := []libp2p.Option{
		libp2p.ListenAddrs(listen),
		libp2p.ConnectionManager(connman),
	}

	if keyb64 := os.Getenv("KEY"); keyb64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(keyb64)
		if err != nil {
			logger.Fatal(err)
		}
		priv, err := crypto.UnmarshalPrivateKey(decoded)
		if err != nil {
			logger.Fatal(err)
		}
		opts = append(opts, libp2p.Identity(priv))
	}

	h, err := libp2p.New(
		ctx,
		opts...,
	)

	if err != nil {
		logger.Fatal(err)
	}

	defer h.Close()

	for _, addr := range h.Addrs() {
		fmt.Printf("%s/ipfs/%s\n", addr, h.ID())
	}
	fmt.Println()

	raftCfg := raft.DefaultConfig()
	raftID := peer.IDB58Encode(h.ID())
	raftCfg.LocalID = raft.ServerID(raftID)
	servers := []raft.Server{raft.Server{
		Suffrage: raft.Voter,
		ID:       raftCfg.LocalID,
		Address:  raft.ServerAddress(raftID),
	}}
	serversCfg := raft.Configuration{servers}

	snapshots := raft.NewInmemSnapshotStore()
	logStore := raft.NewInmemStore()
	tpt, err := libp2praft.NewLibp2pTransport(h, time.Minute)
	if err != nil {
		logger.Fatal(err)
	}

	voter := os.Getenv("VOTER") != ""
	if voter {
		err := raft.BootstrapCluster(raftCfg, logStore, logStore, snapshots, tpt, serversCfg.Clone())
		if err != nil {
			logger.Fatal(err)
		}
		fmt.Println("This is a voter peer and can add other servers")
	} else {
		fmt.Println("This is a staging server! It needs to be added by a voter peer")
	}
	fmt.Println()

	raftCons, err := raft.NewRaft(raftCfg, opLog.FSM(), logStore, logStore, snapshots, tpt)
	if err != nil {
		logger.Fatal(err)
	}

	actor := libp2praft.NewActor(raftCons)
	opLog.SetActor(actor)

	if btp := os.Getenv("BOOTSTRAP"); btp != "" {
		addr, err := multiaddr.NewMultiaddr(btp)
		if err != nil {
			logger.Fatal(err)
		}
		_, dialargs, err := manet.DialArgs(addr)
		if err != nil {
			logger.Fatal(err)
		}

		var urls []string
		for _, u := range h.Addrs() {
			if !manet.IsIPLoopback(u) {
				urls = append(urls,
					fmt.Sprintf(
						"http://%s%s/ipfs/%s", dialargs, u, h.ID(),
					))
			}
		}
		time.AfterFunc(15*time.Second, func() {
			for _, url := range urls {
				fmt.Println("bootsrapping to", url)
				_, err = http.Post(url, "", nil)
				if err != nil {
					logger.Error(err)
				}
			}
		})
	}

	respondError := func(w http.ResponseWriter, err error) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error() + "\n"))
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			if r.URL.Path == "/" {
				goto LIST
			}
			kv := strings.SplitN(r.URL.Path, "/", 2)
			if len(kv) < 2 {
				http.NotFound(w, r)
				return
			}
			key := datastore.NewKey(kv[1])
			value, err := store.Get(key)
			if err != nil {
				respondError(w, err)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(string(value) + "\n"))
			return
		case "POST":
			maddr, err := multiaddr.NewMultiaddr(r.URL.Path)
			if err != nil {
				respondError(w, err)
				return
			}
			addrInf, err := peer.AddrInfoFromP2pAddr(maddr)
			if err != nil {
				respondError(w, err)
				return
			}
			conCtx, conCancel := context.WithTimeout(ctx, 30*time.Second)
			defer conCancel()
			err = h.Connect(conCtx, *addrInf)
			if err != nil {
				respondError(w, err)
				return
			}
			connman.TagPeer(addrInf.ID, "keep", 100)

			id := peer.IDB58Encode(addrInf.ID)
			future := raftCons.AddVoter(
				raft.ServerID(id),
				raft.ServerAddress(id),
				0, 0)
			if future.Error() != nil {
				respondError(w, err)
				return
			}
			fmt.Println("added", maddr)
			w.WriteHeader(http.StatusAccepted)
			return
		case "PUT":
			kv := strings.SplitN(r.URL.Path, "/", 3)
			if len(kv) < 3 {
				http.NotFound(w, r)
				return
			}
			key := kv[1]
			value := kv[2]

			op := &Op{
				Key:    key,
				Value:  value,
				Method: "PUT",
			}

			_, err := opLog.CommitOp(op)
			if err != nil {
				respondError(w, err)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf("%s:%s\n", key, value)))
			return
		case "DELETE":
			kv := strings.SplitN(r.URL.Path, "/", 2)
			if len(kv) < 2 || kv[1] == "" {
				http.NotFound(w, r)
				return
			}
			key := kv[1]
			op := &Op{
				Key:    key,
				Method: "DELETE",
			}
			_, err := opLog.CommitOp(op)
			if err != nil {
				respondError(w, err)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(string(key) + "\n"))
			return
		default:
			http.NotFound(w, r)
			return
		}
	LIST:
		q := query.Query{}
		results, err := store.Query(q)
		if err != nil {
			respondError(w, err)
		}
		w.WriteHeader(http.StatusOK)
		for r := range results.Next() {
			if r.Error != nil {
				w.Write([]byte("error: " + err.Error() + "\n"))
				continue
			}
			w.Write([]byte(fmt.Sprintf("%s:%s\n", r.Key, r.Value)))
		}
	})

	fmt.Println("Get: GET /<key>")
	fmt.Println("Put: PUT /<key>/<value>")
	fmt.Println("Delete: DELETE /<key>")
	fmt.Println("List: GET /")
	fmt.Println("AddVoter: POST /ip4/<ip>/tcp/<port>/ipfs/<pid>")
	fmt.Println()

	lstr, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", httpPort))
	if err != nil {
		logger.Fatal(err)
		return
	}
	defer lstr.Close()
	fmt.Println("listening on:", lstr.Addr())
	fmt.Println()
	http.Serve(lstr, nil)
}
