//go:build unix

package aws

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePlugin puts a session-manager-plugin of the given version first on the
// path: it records what it is run with, says something on stderr, and carries
// a tunnel until it is stopped — or, told to fail, ends at once.
func fakePlugin(t *testing.T, version string, fail bool) string {
	t.Helper()
	dir := t.TempDir()
	record := filepath.Join(dir, "run")
	body := "#!/bin/sh\n" +
		"if [ \"$1\" = --version ]; then echo '" + version + "'; exit 0; fi\n" +
		"printf '%s\\n' \"$@\" > " + record + ".args\n" +
		"printf '%s' \"$AWS_SSM_START_SESSION_RESPONSE\" > " + record + ".session\n" +
		"echo 'Starting session with SessionId: user-0abc' >&2\n"
	if fail {
		body += "echo 'Cannot perform start session: EOF' >&2\nexit 1\n"
	} else {
		body += "exec sleep 600\n"
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, plugin), []byte(body), 0o755))
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return record
}

// tunnels open through a fake Systems Manager in any region.
var tunnels = &ssmTunnels{ssm: func(string) ssmAPI { return fakeSSM{} }}

// The plugin must be there, and new enough to take its session from the
// environment, before an instance is launched for a tunnel.
func TestTunnels_Ready(t *testing.T) {
	fakePlugin(t, "1.2.707.0", false)
	require.NoError(t, tunnels.ready())

	fakePlugin(t, "1.2.497.0", false)
	assert.EqualError(t, tunnels.ready(), "session-manager-plugin is 1.2.497.0: upgrade it to one newer than 1.2.497.0")

	fakePlugin(t, "unknown", false)
	assert.EqualError(t, tunnels.ready(), `session-manager-plugin --version: "unknown" is not a version`)

	t.Setenv("PATH", t.TempDir())
	assert.ErrorContains(t, tunnels.ready(), "session-manager-plugin is not installed")
}

// A tunnel is opened as the AWS CLI opens one: the session started through
// the SDK, then handed to the plugin — through the environment — with the
// region, the operation, the request and the endpoint. Stopped, it is closed.
func TestTunnels_Open(t *testing.T) {
	record := fakePlugin(t, "1.2.707.0", false)
	opened, err := tunnels.open(context.Background(), "eu-north-1", "i-0123")
	require.NoError(t, err)
	assert.Regexp(t, `^http://127\.0\.0\.1:\d+$`, opened.url())
	require.Eventually(t, func() bool { _, err := os.Stat(record + ".session"); return err == nil }, 10*time.Second, 10*time.Millisecond)

	args, err := os.ReadFile(record + ".args")
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSuffix(string(args), "\n"), "\n")
	require.Len(t, lines, 6)
	port := strings.TrimPrefix(opened.url(), "http://127.0.0.1:")
	assert.Equal(t, []string{sessionEnv, "eu-north-1", "StartSession", ""}, lines[:4])
	assert.JSONEq(t, `{"Target":"i-0123","DocumentName":"AWS-StartPortForwardingSession","Parameters":{"portNumber":["8000"],"localPortNumber":["`+port+`"]}}`, lines[4])
	assert.Equal(t, "https://ssm.eu-north-1.amazonaws.com", lines[5])
	session, err := os.ReadFile(record + ".session")
	require.NoError(t, err)
	var handed map[string]string
	require.NoError(t, json.Unmarshal(session, &handed))
	assert.Equal(t, map[string]string{"SessionId": "user-0abc", "TokenValue": "token", "StreamUrl": "wss://ssmmessages.example/i-0123"}, handed)

	assert.False(t, opened.closed())
	opened.close()
	assert.True(t, opened.closed())
	opened.close()
}

// A tunnel the plugin drops is closed, saying what the plugin said.
func TestTunnels_Dropped(t *testing.T) {
	fakePlugin(t, "1.2.707.0", true)
	opened, err := tunnels.open(context.Background(), "eu-north-1", "i-0123")
	require.NoError(t, err)
	require.Eventually(t, opened.closed, 10*time.Second, 10*time.Millisecond)
	assert.Equal(t, "Starting session with SessionId: user-0abc\nCannot perform start session: EOF", opened.reason())
}

func TestNewerVersion(t *testing.T) {
	for _, tc := range []struct {
		version, than string
		newer         bool
	}{
		{"1.2.707.0", "1.2.497.0", true},
		{"1.2.497.0", "1.2.497.0", false},
		{"1.2.497", "1.2.497.0", false},
		{"1.3", "1.2.497.0", true},
		{"1.2.496.9", "1.2.497.0", false},
	} {
		newer, err := newerVersion(tc.version, tc.than)
		require.NoError(t, err)
		assert.Equal(t, tc.newer, newer, tc.version)
	}
	_, err := newerVersion("1.2", "x")
	assert.EqualError(t, err, `"x" is not a version`)
}
