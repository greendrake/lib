package failures

import (
	"errors"
	"fmt"
	"testing"
)

// A failure is news where its cause is another than the last one's, whatever wraps it,
// until a success.
func TestLast(t *testing.T) {
	full, refused := errors.New("no space left"), errors.New("refused")
	var last Last
	for i, step := range []struct {
		err  error
		news bool
	}{
		{fmt.Errorf("creating /a: %w", full), true},
		{fmt.Errorf("creating /b: %w", full), false},
		{refused, true},
		{full, true},
		{nil, false},
		{full, true},
	} {
		if step.err == nil {
			last.Succeeded()
			continue
		}
		if news := last.News(step.err); news != step.news {
			t.Fatalf("step %d: %v news %v", i, step.err, news)
		}
	}
}
