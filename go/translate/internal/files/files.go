// Package files writes what the translator produces: only where the bytes
// differ, and YAML, XML and JSON only once what is about to be written has
// parsed back.
package files

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Digest hashes the inputs something was made from. Truncated because it is
// read by people in diffs and compared by machines, and 64 bits of a SHA-256
// is far past collision relevance for a few thousand strings.
func Digest(parts ...string) string {
	digest := sha256.New()
	for _, part := range parts {
		digest.Write([]byte(part))
		digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))[:16]
}

// Sink is where a writer's bytes go. It reports the paths it found not
// holding them: Write puts the bytes there, Compare only says so — which is
// how a check sees what a run would change without one writer per mode.
type Sink func(path, body string) ([]string, error)

// WriteYAML marshals a value under a header, framed as `#` comments, into a
// sink, and reads it back first.
//
// The invariant the translator exists to keep is that every consumer can load
// what it produces, and the emitter has been observed to write YAML it cannot
// parse. Parsing here costs microseconds on files of this size and turns that
// class of defect into a message naming the file, at the moment it is written,
// instead of a parse error on somebody's next run.
func WriteYAML(sink Sink, path, header string, value any) ([]string, error) {
	var out strings.Builder
	for _, line := range strings.Split(header, "\n") {
		out.WriteString("# " + line + "\n")
	}
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(4)
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var readBack any
	if err := yaml.Unmarshal([]byte(out.String()), &readBack); err != nil {
		return nil, fmt.Errorf("%s: rendered YAML does not parse back: %w", path, err)
	}
	return sink(path, out.String())
}

// Write writes only when the bytes differ, and reports the path when it did.
// A run with nothing to do must touch nothing: a person reading `git status`
// after it should see it empty.
func Write(path, body string) ([]string, error) {
	differs, err := Compare(path, body)
	if err != nil || differs == nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", path, err)
	}
	return []string{path}, nil
}

// Compare writes nothing, and reports the path when its bytes are not body — a
// missing file included.
func Compare(path, body string) ([]string, error) {
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []string{path}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if string(existing) == body {
		return nil, nil
	}
	return []string{path}, nil
}

// Remove deletes the files that exist among paths and reports which did.
func Remove(paths []string) ([]string, error) {
	var removed []string
	for _, path := range paths {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("remove %s: %w", path, err)
		}
		removed = append(removed, path)
	}
	return removed, nil
}

// Scalar writes a string, quoting anything with a line break in it.
//
// A literal block would read better. It is not used because the emitter
// sometimes writes a block with an explicit indentation indicator (`|4-`) that
// it then cannot parse back — on a string that carries nothing unusual, in a
// file whose other blocks come out fine. A generated file that will not load
// is a worse defect than an escaped newline is an eyesore, and a quoted scalar
// has no indentation semantics to get wrong. WriteYAML checks the result
// either way.
func Scalar(text string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.ScalarNode, Value: text}
	if strings.Contains(text, "\n") {
		node.Style = yaml.DoubleQuotedStyle
	}
	return node
}

// Key is a mapping key.
func Key(name string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: name}
}
