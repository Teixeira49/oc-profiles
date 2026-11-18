package ojson

import "testing"

func TestRoundTripPreservesOrderAndFormatting(t *testing.T) {
	in := `{
  "z": 1,
  "a": {
    "y": "<tag> & \"quote\" — ñ\n",
    "b": [],
    "c": {},
    "d": [
      true,
      null,
      1.50
    ]
  }
}`
	v, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != in {
		t.Fatalf("round trip mismatch:\n%s", out)
	}
}

func TestInsertAndSetSorted(t *testing.T) {
	o := NewObject()
	o.Set("description", "d")
	o.Set("mode", "m")
	o.Set("prompt", "p")
	o.SetSorted("model", "x")
	o.SetSorted("variant", "v")
	o.SetSorted("mode", "m2")
	got := o.Keys()
	want := []string{"description", "mode", "model", "prompt", "variant"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keys = %v", got)
		}
	}
	if v, _ := o.GetString("mode"); v != "m2" {
		t.Fatal("SetSorted must replace in place")
	}
	o.Delete("model")
	if o.Has("model") || o.Len() != 4 {
		t.Fatal("delete failed")
	}
}

func TestHTMLEscapingModeRoundTrips(t *testing.T) {
	in := "{\n  \"a\": \"\\u003cb\\u003e \\u0026 x\"\n}"
	if !UsesHTMLEscaping([]byte(in)) || UsesHTMLEscaping([]byte(`{"a": "<b>"}`)) {
		t.Fatal("escaping detection wrong")
	}
	v, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := MarshalOpts(v, true)
	if string(out) != in {
		t.Fatalf("escaped round trip:\n%s", out)
	}
	raw, _ := MarshalOpts(v, false)
	if string(raw) != "{\n  \"a\": \"<b> & x\"\n}" {
		t.Fatalf("raw mode:\n%s", raw)
	}
}
