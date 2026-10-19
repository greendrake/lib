package rpcserver

import (
	"context"
	"encoding/json"
	"fmt"
)

// Emitter pushes event, carrying data, to every live connection of userID, an
// id of the host's type. Delivery is best-effort — a user with no connection,
// or one that drops while the event is in flight, misses it — so emitting
// reports nothing. Data that does not marshal is a mistake in the program, and
// panics.
type Emitter interface {
	Emit(ctx context.Context, userID any, event string, data any)
}

// eventFrame is a push addressed to a user: {type: "event", event, data}.
type eventFrame struct {
	Type  string `json:"type"`
	Event string `json:"event"`
	Data  any    `json:"data"`
}

// dataFrame is a push whose own type names the payload, with no event envelope
// and no addressee: what a Broadcast sends every live socket.
type dataFrame struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// sendData pushes a data frame to this connection.
func (ws *WSConn) sendData(dataType string, data any) {
	ws.Send(mustMarshal(dataFrame{Type: dataType, Data: data}))
}

// mustMarshal is a push on the wire, once for however many sockets it goes to.
// A push that does not marshal is its producer's bug.
func mustMarshal(frame any) []byte {
	msg, err := json.Marshal(frame)
	if err != nil {
		panic(fmt.Sprintf("rpcserver: push does not marshal: %v", err))
	}
	return msg
}
