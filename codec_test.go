package libp2praft

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	consensus "github.com/libp2p/go-libp2p-consensus"
)

type marshable struct {
	A string
}

func (m *marshable) Marshal(w io.Writer) error {
	enc := json.NewEncoder(w)
	return enc.Encode(m)
}

func (m *marshable) Unmarshal(r io.Reader) error {
	dec := json.NewDecoder(r)
	return dec.Decode(m)
}

type testState struct {
	A int
	B string
	C simple
}

type simple struct {
	D int
}

func TestEncodeDecodeSnapshot_Marshable(t *testing.T) {
	m := &marshable{A: "testing"}

	var buf bytes.Buffer
	err := EncodeSnapshot(m, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if str := buf.String(); str != `{"A":"testing"}`+"\n" {
		t.Fatal("expected json encoding: ", str)
	}
	m2 := &marshable{}
	err = DecodeSnapshot(consensus.State(m2), &buf)
	if err != nil {
		t.Fatal(err)
	}
	if m2.A != m.A {
		t.Fatal("bad marshable decoding")
	}

}

func TestEncodeDecodeSnapshot(t *testing.T) {
	var buf bytes.Buffer
	um := &simple{D: 25}
	err := EncodeSnapshot(um, &buf)
	if err != nil {
		t.Fatal(err)
	}
	um2 := &simple{}
	err = DecodeSnapshot(um2, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if um2.D != um.D {
		t.Fatal("bad unmarshable decoding")
	}

	buf.Reset()

	st := &testState{
		A: 5,
		B: "hola",
		C: simple{
			D: 1,
		},
	}

	err = EncodeSnapshot(st, &buf)
	if err != nil {
		t.Fatal(err)
	}

	stmp := consensus.State(&testState{})

	err = DecodeSnapshot(stmp, &buf)
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
