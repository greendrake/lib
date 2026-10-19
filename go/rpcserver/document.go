package rpcserver

import (
	"encoding/json"
	"fmt"
	"sync"
)

// Document is a state stream: the whole of some state as one JSON object, which every
// socket of the Broadcast keeps. An opening socket is sent the document as last
// published, {type, data: {doc}}, and each change after that as an RFC 7386 merge patch
// over it, {type, data: {patch}}: objects merge, null removes a member, anything else
// replaces — so collections are objects keyed by id. What a socket holds is always the
// document.
//
// A document has no null members: a merge patch says removal with null, so a member
// produced as null is dropped, the same as one left out.
type Document struct {
	dataType string
	produce  func() any

	// mu serialises publishing with greeting: a socket's snapshot is neither preceded
	// by a patch, nor overtaken by one computed before it.
	mu    sync.Mutex
	doc   map[string]any
	conns map[*WSConn]struct{}
}

type docFrame struct {
	Doc map[string]any `json:"doc"`
}

type patchFrame struct {
	Patch map[string]any `json:"patch"`
}

// Document publishes produce's state as the stream dataType: produced now, so that every
// socket is greeted with a document, and again on each Refresh. produce runs under the
// Document's lock, so it must not itself publish, and a producer must not hold a lock
// produce takes while it calls Refresh.
func (b *Broadcast) Document(dataType string, produce func() any) *Document {
	d := &Document{dataType: dataType, produce: produce, conns: map[*WSConn]struct{}{}}
	d.doc = d.render()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.docs = append(b.docs, d)
	// A socket opening meanwhile either finds the document listed or is in the set
	// walked here; greeting it twice would only resend the same document.
	for ws := range b.conns {
		d.greet(ws)
	}
	return d
}

// Refresh publishes the state as produce now has it: what changed since it was last
// published goes to every socket as one patch, and nothing goes when nothing changed.
// So a producer calls it on any change it knows of; a call too many costs a diff.
func (d *Document) Refresh() {
	d.mu.Lock()
	defer d.mu.Unlock()
	next := d.render()
	patch, changed := mergePatch(d.doc, next)
	if !changed {
		return
	}
	d.doc = next
	msg := mustMarshal(dataFrame{Type: d.dataType, Data: patchFrame{patch}})
	for ws := range d.conns {
		ws.Send(msg)
	}
}

// greet has a socket keep the document from now on.
func (d *Document) greet(ws *WSConn) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.conns[ws] = struct{}{}
	ws.sendData(d.dataType, docFrame{d.doc})
}

func (d *Document) leave(ws *WSConn) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.conns, ws)
}

// render is the state as decoded JSON, in which form it is diffed. A state that is not
// a JSON object is no document, and is the producer's bug.
func (d *Document) render() map[string]any {
	raw, err := json.Marshal(d.produce())
	if err != nil {
		panic(fmt.Sprintf("rpcserver: document %s does not marshal: %v", d.dataType, err))
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil || doc == nil {
		panic(fmt.Sprintf("rpcserver: document %s is not a JSON object: %s", d.dataType, raw))
	}
	dropNulls(doc)
	return doc
}

// dropNulls removes the null members of object and of the objects within it.
func dropNulls(object map[string]any) {
	for key, value := range object {
		switch value := value.(type) {
		case nil:
			delete(object, key)
		case map[string]any:
			dropNulls(value)
		}
	}
}
