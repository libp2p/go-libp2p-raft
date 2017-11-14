package libp2praft

import (
	"encoding/json"
	"testing"

	consensus "github.com/libp2p/go-libp2p-consensus"
)

type marshable struct {
	A string
}

func (m *marshable) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func (m *marshable) Unmarshal(d []byte) error {
	return json.Unmarshal(d, m)
}

type testState struct {
	A int
	B string
	C simple
}

type simple struct {
	D int
}

func TestEncodeDecodeSnapshot(t *testing.T) {
	m := &marshable{A: "testing"}
	res, err := EncodeSnapshot(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(res) != `{"A":"testing"}` {
		t.Fatal("expected json encoding: ", res)
	}
	t.Log(string(res))
	m2 := &marshable{}
	err = DecodeSnapshot(res, consensus.State(m2))
	if err != nil {
		t.Fatal(err)
	}
	if m2.A != m.A {
		t.Fatal("bad marshable decoding")
	}

	um := &simple{D: 25}
	res2, err := EncodeSnapshot(um)
	if err != nil {
		t.Fatal(err)
	}
	um2 := &simple{}
	err = DecodeSnapshot(res2, um2)
	if err != nil {
		t.Fatal(err)
	}
	if um2.D != um.D {
		t.Fatal("bad unmarshable decoding")
	}
}

func TestEncodeDecodeState(t *testing.T) {
	st := &testState{
		A: 5,
		B: "hola",
		C: simple{
			D: 1,
		},
	}

	bytes, err := encodeState(stateWrapper{st})
	if err != nil {
		t.Fatal(err)
	}

	stmp := consensus.State(&testState{})

	err = decodeState(bytes, &stateWrapper{stmp})
	if err != nil {
		t.Fatal(err)
	}
	newst := stmp.(*testState)

	t.Logf("st: %p newst: %p", &st, &newst)
	if &st == &newst {
		t.Fatal("both states are the same pointer")
	}
	t.Logf("st: %v newst: %v", &st, &newst)

	if st.A != newst.A || st.B != newst.B || st.C.D != newst.C.D {
		t.Error("dup object is different")
	}

	st.B = "adios"
	if newst.B != "hola" {
		t.Fatal("side modifications")
	}

	st.C.D = 6
	if newst.C.D == 6 {
		t.Fatal("side modifications in nested object")
	}
}
