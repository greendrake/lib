package rpcserver

import (
	"errors"
	"fmt"
	"testing"
)

func TestAnswer(t *testing.T) {
	errA, errB := errors.New("a"), errors.New("b")
	codes := []ErrorCode{{errA, "A"}, {errB, "B"}}
	for err, want := range map[error]string{
		fmt.Errorf("wrapped: %w", errB):                "B",
		NewAPIError("OWN"):                             "OWN",
		fmt.Errorf("wrapped: %w", NewAPIError("LATE")): "LATE",
	} {
		var apiErr *APIError
		if !errors.As(Answer("m", err, codes), &apiErr) || apiErr.Code != want {
			t.Errorf("%v: %v, want %s", err, apiErr, want)
		}
	}
}

func TestAnswer_Fault(t *testing.T) {
	fault := errors.New("db down")
	err := Answer("thing.get", fault, []ErrorCode{{errors.New("other"), "OTHER"}})
	var apiErr *APIError
	if errors.As(err, &apiErr) || !errors.Is(err, fault) || err.Error() != "thing.get: db down" {
		t.Fatalf("a fault goes to the dispatcher as itself, under the method: %v", err)
	}
}
