package aws

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// A tunnel to vLLM on the instance is an SSM port-forwarding session, opened
// as the AWS CLI's `ssm start-session` opens one: StartSession through the
// SDK, then the session handed to the CLI's Session Manager plugin, which
// carries the port.

// tunneler opens tunnels to instances, and says what it needs to before an
// instance is launched for one.
type tunneler interface {
	ready() error
	open(ctx context.Context, region, instance string) (tunnel, error)
}

// tunnel is one open tunnel.
type tunnel interface {
	// url is the tunnel's local end.
	url() string
	// closed reports the tunnel gone, and reason what it said as it went.
	closed() bool
	reason() string
	close()
}

const (
	// plugin is the AWS CLI's Session Manager plugin.
	plugin = "session-manager-plugin"
	// pluginWithoutEnv is the last plugin version that takes the session only
	// on its command line; every later one reads it from sessionEnv, which
	// keeps the session's token out of the process list.
	pluginWithoutEnv = "1.2.497.0"
	sessionEnv       = "AWS_SSM_START_SESSION_RESPONSE"
	// portForward is the SSM document that forwards a port.
	portForward = "AWS-StartPortForwardingSession"
)

// ssmTunnels opens tunnels through Systems Manager, in each region through
// its client.
type ssmTunnels struct {
	ssm func(region string) ssmAPI
}

// ready refuses a missing plugin, or one too old to take its session from the
// environment.
func (t *ssmTunnels) ready() error {
	out, err := exec.Command(plugin, "--version").Output()
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%s is not installed: the tunnel to the instance runs through the AWS CLI's Session Manager plugin — https://docs.awssdk.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html", plugin)
	}
	if err != nil {
		return fmt.Errorf("%s --version: %w", plugin, err)
	}
	version := strings.TrimSpace(string(out))
	newer, err := newerVersion(version, pluginWithoutEnv)
	if err != nil {
		return fmt.Errorf("%s --version: %w", plugin, err)
	}
	if !newer {
		return fmt.Errorf("%s is %s: upgrade it to one newer than %s", plugin, version, pluginWithoutEnv)
	}
	return nil
}

// newerVersion reports whether a dotted version is newer than another.
func newerVersion(version, than string) (bool, error) {
	parse := func(v string) ([]int, error) {
		var out []int
		for _, part := range strings.Split(v, ".") {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("%q is not a version", v)
			}
			out = append(out, n)
		}
		return out, nil
	}
	a, err := parse(version)
	if err != nil {
		return false, err
	}
	b, err := parse(than)
	if err != nil {
		return false, err
	}
	for len(a) < len(b) {
		a = append(a, 0)
	}
	for len(b) < len(a) {
		b = append(b, 0)
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i], nil
		}
	}
	return false, nil
}

func (t *ssmTunnels) open(ctx context.Context, region, instance string) (tunnel, error) {
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	client := t.ssm(region)
	parameters := map[string][]string{"portNumber": {strconv.Itoa(vllmPort)}, "localPortNumber": {strconv.Itoa(port)}}
	session, err := client.StartSession(ctx, &ssm.StartSessionInput{Target: awssdk.String(instance), DocumentName: awssdk.String(portForward), Parameters: parameters})
	if err != nil {
		return nil, err
	}
	// abandon ends a session no plugin took, so that it does not linger on
	// the instance's agent, and reports why.
	abandon := func(err error) error {
		_, terminateErr := client.TerminateSession(context.Background(), &ssm.TerminateSessionInput{SessionId: session.SessionId})
		return errors.Join(err, terminateErr)
	}
	endpoint, err := ssm.NewDefaultEndpointResolverV2().ResolveEndpoint(ctx, ssm.EndpointParameters{Region: awssdk.String(region)})
	if err != nil {
		return nil, abandon(err)
	}
	response, err := json.Marshal(map[string]string{
		"SessionId": awssdk.ToString(session.SessionId), "TokenValue": awssdk.ToString(session.TokenValue), "StreamUrl": awssdk.ToString(session.StreamUrl),
	})
	if err != nil {
		return nil, abandon(err)
	}
	request, err := json.Marshal(map[string]any{"Target": instance, "DocumentName": portForward, "Parameters": parameters})
	if err != nil {
		return nil, abandon(err)
	}
	// The plugin's arguments as the CLI gives them: the session — here the
	// variable holding it — the region, the operation, the profile (the
	// plugin takes credentials from the environment as the SDK did), the
	// request and the endpoint.
	cmd := exec.Command(plugin, sessionEnv, region, "StartSession", "", string(request), endpoint.URI.String())
	cmd.Env = append(os.Environ(), sessionEnv+"="+string(response))
	p := &pluginTunnel{local: fmt.Sprintf("http://127.0.0.1:%d", port), cmd: cmd, done: make(chan struct{})}
	// Its per-connection chatter is dropped; what it says as it fails is kept.
	cmd.Stderr = &p.stderr
	// A process group of its own: an interrupt reaches the lease, which
	// decides when the tunnel closes.
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return nil, abandon(err)
	}
	go func() {
		_ = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

// freePort is a local port nothing listens on.
func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// pluginTunnel is a tunnel the plugin carries.
type pluginTunnel struct {
	local  string
	cmd    *exec.Cmd
	stderr lockedBuffer
	done   chan struct{}
}

func (p *pluginTunnel) url() string { return p.local }

func (p *pluginTunnel) closed() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *pluginTunnel) reason() string { return strings.TrimSpace(p.stderr.String()) }

func (p *pluginTunnel) close() {
	if !p.closed() {
		stop(p.cmd)
		<-p.done
	}
}

// lockedBuffer is a buffer a process writes while another goroutine reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(data)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
