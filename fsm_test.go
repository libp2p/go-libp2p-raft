package libp2praft

import (
	"testing"

	consensus "github.com/libp2p/go-libp2p-consensus"
)

// Lets assume this would be our state
type testState struct {
	A int
	B string
	C simple
}

type simple struct {
	D int
}

func TestCodecs(t *testing.T) {
	st := testState{
		A: 5,
		B: "hola",
		C: simple{
			D: 1,
		},
	}

	bytes, err := encodeState(st)
	if err != nil {
		t.Fatal(err)
	}

	stmp := consensus.State(testState{})

	err = decodeState(bytes, &stmp)
	if err != nil {
		t.Fatal(err)
	}
	newst := stmp.(testState)

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
