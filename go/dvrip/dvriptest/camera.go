// Package dvriptest is a DVRIP camera to run a client against: it answers logins,
// configuration, abilities, reports, the clock, reboots, exports and title bitmaps from
// a recorded device's replies, and streams a recording, as the vendor's
// protocol has a camera do. Tests start one in-process; cmd/fakecam runs one on its own.
package dvriptest

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"greendrake.nz/go/dvrip/internal/wire"
)

// Options configure a camera. Their zero values are the camera's defaults.
type Options struct {
	// The password of the account it takes, "admin"; empty, as the cameras ship, if
	// unset.
	Password string
	// The keep-alive interval it hands out, in seconds; 20 if 0.
	AliveInterval int
	// Replies replaced, or removed where the value is nil, by query name.
	Replies map[string]json.RawMessage
	// Commands answered with a failure instead, by name, or by name and action as
	// "OPMonitor.Start".
	Refuse map[string]int
	// Commands never answered, keyed as Refuse is; "login" for the login.
	Silent map[string]bool
	// The status writes of a configuration are answered with, by name; 100 for the
	// rest.
	SetStatus map[string]int
	// The configurations whose writes the camera applies by restarting, as one that
	// restarts its network on an address change: it answers the write, then drops every
	// connection as a reboot does.
	Restarts map[string]bool
	// The configurations whose writes the camera answers as taken but keeps as it had
	// them, as one does a value it does not take.
	Ignores map[string]bool
	// When positive, control replies go out in packets of this many bytes.
	Chunk int
	// Replaces the login reply.
	LoginReply map[string]any
	// What a configuration export returns.
	Export []byte
	// Media every monitor carries instead of the recording, cut into packets
	// by Split (one packet if nil), and the message code it goes out under,
	// the protocol's MonitorData (1412) if 0.
	Media     []byte
	Split     func([]byte) [][]byte
	MediaCode uint16
	// Plays the media at its frame rate, over and over, rather than all at once.
	Realtime bool
	// How long a reboot keeps the camera unreachable.
	RebootPause time.Duration
}

// firstSession is the session ID the first login gets; each next one gets the next.
const firstSession = 0x57

// recordingPacket is the size of the media packets a recording goes out in, as devices
// keep theirs.
const recordingPacket = 16 << 10

// Camera speaks the protocol from the device's end: a control connection per login
// and media connections claimed for those logins.
type Camera struct {
	device  string
	options Options
	// Where protocol violations the camera notices go.
	report func(format string, args ...any)

	mu       sync.Mutex
	listener net.Listener
	address  string
	replies  map[string]json.RawMessage
	sessions uint32
	// The camera's clock: set at the real instant at.
	clockSet, clockAt time.Time
	received          []Command
	// The open media connections, by what they were claimed for.
	claims map[claim]*media
	// The control connections, by the session each logged in.
	controls map[net.Conn]uint32
	conns    map[net.Conn]struct{}
	// The commands held unanswered, by name: each is answered once its gate closes.
	holds map[string]chan struct{}
	// The channel title it was last sent to draw; nil until one is.
	title  *wire.Bitmap
	closed bool
}

// Command is one the camera received on a control connection, or the claim a media
// connection opened with.
type Command struct {
	// Its message code.
	Code uint16
	Name string
	// The command's JSON body, field by field.
	Body map[string]json.RawMessage
}

// Payload decodes what the command carried under its name.
func (c Command) Payload(v any) error {
	return json.Unmarshal(c.Body[c.Name], v)
}

// claim is what a media connection is claimed for. Claim, Start and Stop all name it.
type claim struct {
	session uint32
	channel int
	stream  string
}

type media struct {
	conn    net.Conn
	started chan struct{}
	start   sync.Once
}

// monitorOp is an OPMonitor command's body.
type monitorOp struct {
	Action    string
	Parameter struct {
		Channel    int
		StreamType string
	}
}

// Start serves the recorded device on a port of its own on the loopback interface.
func Start(device string, report func(format string, args ...any), options Options) (*Camera, error) {
	return Listen("127.0.0.1:0", device, report, options)
}

// Listen serves the recorded device on address.
func Listen(address, device string, report func(format string, args ...any), options Options) (*Camera, error) {
	replies, err := Replies(device)
	if err != nil {
		return nil, err
	}
	for name, value := range options.Replies {
		if value == nil {
			delete(replies, name)
		} else {
			replies[name] = value
		}
	}
	c := &Camera{
		device:   device,
		options:  options,
		report:   report,
		replies:  replies,
		sessions: firstSession,
		claims:   map[claim]*media{},
		controls: map[net.Conn]uint32{},
		conns:    map[net.Conn]struct{}{},
		holds:    map[string]chan struct{}{},
	}
	c.setClock(wallNow())
	if recorded, known := replies["OPTimeQuery"]; known {
		var text string
		if err := json.Unmarshal(recorded, &text); err != nil {
			return nil, fmt.Errorf("dvriptest: %s's OPTimeQuery: %w", device, err)
		}
		set, err := time.Parse(time.DateTime, text)
		if err != nil {
			return nil, fmt.Errorf("dvriptest: %s's OPTimeQuery: %w", device, err)
		}
		c.setClock(set)
	}
	if err := c.listen(address); err != nil {
		return nil, err
	}
	return c, nil
}

// wallNow is this machine's wall clock, read as the device reads its own: in no zone.
func wallNow() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), 0, time.UTC)
}

func (c *Camera) setClock(t time.Time) {
	c.clockSet, c.clockAt = t, time.Now()
}

func (c *Camera) listen(address string) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	c.listener = listener
	c.address = listener.Addr().String()
	go c.accept(listener)
	return nil
}

// Address is where the camera listens, as host:port.
func (c *Camera) Address() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.address
}

// Close stops the camera, closing every connection and answering what it holds.
func (c *Camera) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.listener.Close()
	for conn := range c.conns {
		conn.Close()
	}
	for name, gate := range c.holds {
		close(gate)
		delete(c.holds, name)
	}
}

// Title is the bitmap the camera was last sent to draw as the channel title, if any: a
// dvrip.Bitmap's width, height and rows.
func (c *Camera) Title() (width, height int, rows []byte, drawn bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.title == nil {
		return 0, 0, nil, false
	}
	return c.title.Width, c.title.Height, c.title.Rows, true
}

// Hold leaves the commands received under name unanswered until Release: for a test to
// see what its client does while it waits on the camera.
func (c *Camera) Hold(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, held := c.holds[name]; !held {
		c.holds[name] = make(chan struct{})
	}
}

// Release answers the commands held under name, and those received from then on.
func (c *Camera) Release(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gate, held := c.holds[name]; held {
		close(gate)
		delete(c.holds, name)
	}
}

// Commands returns what was received under name.
func (c *Camera) Commands(name string) []Command {
	c.mu.Lock()
	defer c.mu.Unlock()
	var found []Command
	for _, command := range c.received {
		if command.Name == name {
			found = append(found, command)
		}
	}
	return found
}

// Overlay lays body over the configuration the camera keeps under name, as Config.Set
// lays a write over the device's: for a test to change the camera while it runs,
// sending only what it changes.
func (c *Camera) Overlay(name string, body json.RawMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	current, known := c.replies[name]
	if !known {
		return fmt.Errorf("dvriptest: %s keeps no %s", c.device, name)
	}
	merged, err := wire.Overlay(current, body)
	if err != nil {
		return fmt.Errorf("dvriptest: %s: %w", name, err)
	}
	c.replies[name] = merged
	return nil
}

// pushBody is what a message nobody asked for carries.
var pushBody = []byte("{\"Name\":\"AlarmInfo\"}\n\x00")

// Push sends a message nobody asked for on every control connection.
func (c *Camera) Push(code uint16) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for conn, session := range c.controls {
		conn.Write(wire.AppendPacket(nil, wire.Header{Session: session, Code: wire.Code(code)}, pushBody))
	}
}

func (c *Camera) accept(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		c.mu.Lock()
		c.conns[conn] = struct{}{}
		c.mu.Unlock()
		go func() {
			defer c.forget(conn)
			c.serve(conn)
		}()
	}
}

func (c *Camera) forget(conn net.Conn) {
	conn.Close()
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.conns, conn)
	delete(c.controls, conn)
}

func (c *Camera) serve(conn net.Conn) {
	p, err := wire.ReadPacket(conn)
	if err != nil {
		return
	}
	switch p.Code {
	case wire.Login:
		c.serveControl(conn, p)
	case wire.MonitorClaim:
		c.serveMedia(conn, p)
	default:
		c.report("dvriptest: a connection opened with message %d", p.Code)
	}
}

func (c *Camera) serveControl(conn net.Conn, login wire.Packet) {
	var credentials struct{ UserName, PassWord string }
	if err := json.Unmarshal(wire.TrimJSON(login.Data), &credentials); err != nil {
		c.report("dvriptest: login body: %v", err)
		return
	}
	if c.options.Silent["login"] {
		io.Copy(io.Discard, conn)
		return
	}
	alive := c.options.AliveInterval
	if alive == 0 {
		alive = 20
	}
	c.mu.Lock()
	session := c.sessions
	c.sessions++
	c.mu.Unlock()
	answer := c.options.LoginReply
	switch {
	case answer != nil:
	case credentials.UserName != "admin":
		answer = map[string]any{"Name": "", "Ret": 205, "SessionID": "0x00000000"}
	case credentials.PassWord != wire.SofiaHash(c.options.Password):
		answer = map[string]any{"Name": "", "Ret": 203, "SessionID": "0x00000000"}
	default:
		answer = map[string]any{
			"AliveInterval": alive,
			"ChannelNum":    1,
			"DeviceType ":   "IPC",
			"ExtraChannel":  0,
			"Ret":           100,
			"SessionID":     sessionID(session),
		}
		c.mu.Lock()
		c.controls[conn] = session
		c.mu.Unlock()
	}
	c.reply(conn, session, login.Code, answer)
	for {
		p, err := wire.ReadPacket(conn)
		if err != nil {
			return
		}
		c.handle(conn, session, p)
	}
}

func sessionID(session uint32) string {
	return fmt.Sprintf("0x%08X", session)
}

// handlers answer a command by its key; what they return is added to the reply. A
// command no handler takes and no reply names is answered with success alone.
var handlers = map[string]func(c *Camera, session uint32, command Command) map[string]any{
	"OPMonitor.Start": (*Camera).startMonitor,
	"OPMonitor.Stop":  (*Camera).stopMonitor,
	"OPTimeSetting":   (*Camera).setTime,
	"OPTimeQuery":     (*Camera).timeQuery,
}

// queries are the codes whose commands read a reply by name.
var queries = map[wire.Code]bool{wire.ConfigGet: true, wire.Ability: true, wire.SystemInfo: true}

func (c *Camera) handle(conn net.Conn, session uint32, p wire.Packet) {
	if p.Code == wire.ConfigExport {
		c.write(conn, session, p.Code+1, c.options.Export)
		return
	}
	// Its body is a bitmap rather than JSON.
	if p.Code == wire.TitleBitmapSet {
		c.drawTitle(conn, session, p)
		return
	}
	command, ok := c.record(p)
	if !ok {
		return
	}
	key := commandKey(command)
	if c.options.Silent[key] {
		return
	}
	c.mu.Lock()
	gate := c.holds[command.Name]
	c.mu.Unlock()
	if gate != nil {
		<-gate
	}
	answer := map[string]any{"Name": command.Name, "Ret": 100, "SessionID": sessionID(session)}
	ret, refused := c.options.Refuse[key]
	switch {
	case refused:
		answer["Ret"] = ret
	case p.Code == wire.ConfigSet:
		if status, set := c.options.SetStatus[command.Name]; set {
			answer["Ret"] = status
		}
		if !c.options.Ignores[command.Name] {
			c.mu.Lock()
			c.replies[command.Name] = command.Body[command.Name]
			c.mu.Unlock()
		}
	case queries[p.Code]:
		c.mu.Lock()
		value, known := c.replies[command.Name]
		c.mu.Unlock()
		if known {
			answer[command.Name] = value
		} else {
			answer["Ret"] = 607
		}
	case handlers[key] != nil:
		maps.Copy(answer, handlers[key](c, session, command))
	}
	c.reply(conn, session, p.Code, answer)
	if (key == "OPMachine.Reboot" || p.Code == wire.ConfigSet && c.options.Restarts[command.Name]) && !refused {
		c.reboot()
	}
}

// drawTitle keeps the title bitmap a client sent, and answers it as devices do.
func (c *Camera) drawTitle(conn net.Conn, session uint32, p wire.Packet) {
	title, err := wire.DecodeBitmap(p.Data)
	if err != nil {
		c.report("dvriptest: %v", err)
		return
	}
	c.mu.Lock()
	c.title = &title
	c.mu.Unlock()
	c.reply(conn, session, p.Code, map[string]any{"Name": "", "Ret": 100, "SessionID": sessionID(session)})
}

func (c *Camera) startMonitor(session uint32, command Command) map[string]any {
	key, ok := c.claimOf(session, command)
	if !ok {
		return nil
	}
	c.mu.Lock()
	m := c.claims[key]
	c.mu.Unlock()
	if m == nil {
		c.report("dvriptest: %v started without a claim", key)
		return nil
	}
	m.start.Do(func() { close(m.started) })
	return nil
}

func (c *Camera) stopMonitor(session uint32, command Command) map[string]any {
	key, ok := c.claimOf(session, command)
	if !ok {
		return nil
	}
	c.mu.Lock()
	m := c.claims[key]
	delete(c.claims, key)
	c.mu.Unlock()
	if m != nil {
		// After the reply, as devices do it.
		defer m.conn.Close()
	}
	return nil
}

func (c *Camera) setTime(_ uint32, command Command) map[string]any {
	var text string
	if err := command.Payload(&text); err != nil {
		c.report("dvriptest: OPTimeSetting: %v", err)
		return nil
	}
	set, err := time.Parse(time.DateTime, text)
	if err != nil {
		c.report("dvriptest: OPTimeSetting: %v", err)
		return nil
	}
	c.mu.Lock()
	c.setClock(set)
	c.mu.Unlock()
	return nil
}

func (c *Camera) timeQuery(uint32, Command) map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return map[string]any{"OPTimeQuery": c.clockSet.Add(time.Since(c.clockAt)).Format(time.DateTime)}
}

// reboot drops every connection and stays unreachable for the reboot pause.
func (c *Camera) reboot() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.listener.Close()
	for conn := range c.conns {
		conn.Close()
	}
	clear(c.claims)
	address := c.address
	time.AfterFunc(c.options.RebootPause, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.closed {
			return
		}
		if err := c.listen(address); err != nil {
			c.report("dvriptest: listening again after a reboot: %v", err)
		}
	})
}

// claimOf reads what an OPMonitor command names.
func (c *Camera) claimOf(session uint32, command Command) (claim, bool) {
	var op monitorOp
	if err := command.Payload(&op); err != nil {
		c.report("dvriptest: %s: %v", command.Name, err)
		return claim{}, false
	}
	return claim{session: session, channel: op.Parameter.Channel, stream: op.Parameter.StreamType}, true
}

func (c *Camera) serveMedia(conn net.Conn, p wire.Packet) {
	command, ok := c.record(p)
	if !ok {
		return
	}
	var id string
	if err := json.Unmarshal(command.Body["SessionID"], &id); err != nil {
		c.report("dvriptest: claim's session: %v", err)
		return
	}
	session, err := strconv.ParseUint(id, 0, 32)
	if err != nil {
		c.report("dvriptest: claim for session %q", id)
		return
	}
	key, ok := c.claimOf(uint32(session), command)
	if !ok {
		return
	}
	reply := func(ret int) {
		c.reply(conn, key.session, wire.MonitorClaim, map[string]any{"Name": command.Name, "Ret": ret, "SessionID": id})
	}
	if c.options.Silent[commandKey(command)] {
		io.Copy(io.Discard, conn)
		return
	}
	if ret, refused := c.options.Refuse[commandKey(command)]; refused {
		reply(ret)
		return
	}
	m := &media{conn: conn, started: make(chan struct{})}
	c.mu.Lock()
	_, taken := c.claims[key]
	if !taken {
		c.claims[key] = m
	}
	c.mu.Unlock()
	if taken {
		c.report("dvriptest: %v claimed while already claimed", key)
		return
	}
	reply(100)
	// The connection stays open until the client or a Stop closes it, whatever was sent.
	gone := make(chan struct{})
	go func() {
		var b [1]byte
		conn.Read(b[:])
		m.start.Do(func() { close(m.started) })
		close(gone)
	}()
	defer func() { <-gone }()
	<-m.started
	code := wire.Code(c.options.MediaCode)
	if code == 0 {
		code = wire.MonitorData
	}
	send := func(data []byte) bool {
		_, err := conn.Write(wire.AppendPacket(nil, wire.Header{Session: key.session, Code: code}, data))
		return err == nil
	}
	stream, split := c.options.Media, c.options.Split
	if stream == nil {
		var err error
		if stream, err = Recording(); err != nil {
			c.report("dvriptest: %v", err)
			return
		}
		split = splitRecording
	}
	if c.options.Realtime {
		c.play(stream, send)
		return
	}
	chunks := [][]byte{stream}
	if split != nil {
		chunks = split(stream)
	}
	for _, chunk := range chunks {
		if !send(chunk) {
			return
		}
	}
}

// splitRecording cuts a recording into packets of the size devices send.
func splitRecording(b []byte) [][]byte {
	var chunks [][]byte
	for len(b) > 0 {
		n := min(recordingPacket, len(b))
		chunks = append(chunks, b[:n])
		b = b[n:]
	}
	return chunks
}

// play sends a recording a frame at a time, each video frame a frame period after the
// one before at the rate the latest key frame states, and starts again from its first
// key frame once it is through, until the connection is closed.
func (c *Camera) play(stream []byte, send func([]byte) bool) {
	next := time.Now()
	var period time.Duration
	for {
		var frames wire.Assembler
		frames.Push(stream)
		for {
			raw, whole, err := frames.Next()
			if err != nil {
				c.report("dvriptest: recording: %v", err)
				return
			}
			if !whole {
				break
			}
			if raw.Kind == wire.FrameVideoI {
				period = time.Second / time.Duration(wire.KeyframeRate(raw.Header))
			}
			if raw.Kind == wire.FrameVideoI || raw.Kind == wire.FrameVideoP {
				time.Sleep(time.Until(next))
				next = next.Add(period)
			}
			if !send(raw.Bytes) {
				return
			}
		}
	}
}

func (c *Camera) record(p wire.Packet) (Command, bool) {
	command := Command{Code: uint16(p.Code)}
	if err := json.Unmarshal(wire.TrimJSON(p.Data), &command.Body); err != nil {
		c.report("dvriptest: message %d body %q: %v", p.Code, p.Data, err)
		return command, false
	}
	if err := json.Unmarshal(command.Body["Name"], &command.Name); err != nil {
		c.report("dvriptest: message %d name: %v", p.Code, err)
		return command, false
	}
	c.mu.Lock()
	c.received = append(c.received, command)
	c.mu.Unlock()
	return command, true
}

// commandKey names a command for Refuse and Silent: its name, and its action if it
// carries one.
func commandKey(command Command) string {
	var op struct{ Action string }
	if json.Unmarshal(command.Body[command.Name], &op) == nil && op.Action != "" {
		return command.Name + "." + op.Action
	}
	return command.Name
}

// reply answers a request with its code plus one, as devices do.
func (c *Camera) reply(conn net.Conn, session uint32, request wire.Code, answer map[string]any) {
	body, err := wire.EncodeJSON(answer)
	if err != nil {
		c.report("dvriptest: encoding a reply: %v", err)
		return
	}
	c.write(conn, session, request+1, body)
}

func (c *Camera) write(conn net.Conn, session uint32, reply wire.Code, data []byte) {
	chunk := c.options.Chunk
	if chunk <= 0 || len(data) <= chunk {
		conn.Write(wire.AppendPacket(nil, wire.Header{Session: session, Code: reply}, data))
		return
	}
	total := (len(data) + chunk - 1) / chunk
	var out []byte
	for i := range total {
		part := data[i*chunk : min((i+1)*chunk, len(data))]
		out = wire.AppendPacket(out, wire.Header{Session: session, Sequence: uint32(i), Total: uint8(total), Current: uint8(i), Code: reply}, part)
	}
	conn.Write(out)
}

// String names a claim in reports.
func (k claim) String() string {
	return strings.Join([]string{sessionID(k.session), strconv.Itoa(k.channel), k.stream}, "/")
}
