package libp2praft

import "testing"

// Lets assume this would be our state
type testState struct {
	A int
	B string
	C simple
}

type simple struct {
	D int
}

// This should cover encode/decode too
func TestDupState(t *testing.T) {
	st := testState{
		A: 5,
		B: "hola",
		C: simple{
			D: 1,
		},
	}

	stmp, err := dupState(st)
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
		t.Error("Dup object is different")
	}

	st.B = "adios"
	if newst.B != "hola" {
		t.Fatal("Side modifications")
	}

	st.C.D = 6
	if newst.C.D == 6 {
		t.Fatal("Side modifications in nested object")
	}
}
