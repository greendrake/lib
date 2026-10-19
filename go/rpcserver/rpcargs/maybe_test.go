package rpcargs

import (
	"encoding/json"
	"testing"
)

func TestMaybe_Absent(t *testing.T) {
	type doc struct {
		Price Maybe[float64] `json:"price,omitempty"`
	}
	var d doc
	if err := json.Unmarshal([]byte(`{}`), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.Price.Set {
		t.Fatal("expected Set=false for absent field")
	}
}

func TestMaybe_Null(t *testing.T) {
	type doc struct {
		Price Maybe[float64] `json:"price"`
	}
	var d doc
	if err := json.Unmarshal([]byte(`{"price":null}`), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !d.Price.Set || !d.Price.Null {
		t.Fatalf("expected Set=true, Null=true; got %+v", d.Price)
	}
	if d.Price.HasValue() {
		t.Fatal("HasValue should be false when Null")
	}
}

func TestMaybe_Value(t *testing.T) {
	type doc struct {
		Price Maybe[float64] `json:"price"`
	}
	var d doc
	if err := json.Unmarshal([]byte(`{"price":12.5}`), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !d.Price.Set || d.Price.Null || d.Price.Value != 12.5 {
		t.Fatalf("expected Set=true, Null=false, Value=12.5; got %+v", d.Price)
	}
	if !d.Price.HasValue() {
		t.Fatal("HasValue should be true for a concrete value")
	}
}

func TestMaybe_Marshal(t *testing.T) {
	for want, m := range map[string]Maybe[float64]{
		"null": {},
		"12.5": {Set: true, Value: 12.5},
	} {
		if got, err := json.Marshal(m); err != nil || string(got) != want {
			t.Errorf("%+v: %s, %v, want %s", m, got, err, want)
		}
	}
	if got, _ := json.Marshal(Maybe[float64]{Set: true, Null: true, Value: 1}); string(got) != "null" {
		t.Errorf("a cleared field: %s", got)
	}
}
