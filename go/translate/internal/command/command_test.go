package command

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func scoped(t *testing.T, args ...string) *Scope {
	t.Helper()
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	scope := NewScope(flags)
	require.NoError(t, flags.Parse(args))
	return scope
}

// --retranslate is a comma-separated list: each prefix trimmed, an empty one
// dropped.
func TestScope_RetranslateSplitting(t *testing.T) {
	opts := scoped(t, "-retranslate", " ui:common., ,store:listing. ,").Options()
	assert.Equal(t, []string{"ui:common.", "store:listing."}, opts.Retranslate)
	assert.Nil(t, scoped(t, "-retranslate", " , ").Options().Retranslate)
}

// --locale narrows a run to one locale, and without it a run is every
// locale's; every run logs its lines.
func TestScope_LocaleAndLog(t *testing.T) {
	scope := scoped(t, "-locale", "de", "-verbose")
	opts := scope.Options()
	assert.Equal(t, []string{"de"}, opts.Only)
	assert.NotNil(t, opts.Log)
	assert.True(t, scope.Verbose())

	unscoped := scoped(t)
	assert.Nil(t, unscoped.Options().Only)
	assert.Nil(t, unscoped.Options().Retranslate)
	assert.False(t, unscoped.Verbose())
}

// A comma-separated flag is read value by value, trimmed, empties dropped.
func TestSplit(t *testing.T) {
	assert.Equal(t, []string{"eu-north-1", "eu-south-2"}, Split("eu-north-1, eu-south-2 ,"))
	assert.Nil(t, Split(" , "))
}

// A list flag collects every value it is given, in order.
func TestList(t *testing.T) {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	var list List
	flags.Var(&list, "run", "")
	require.NoError(t, flags.Parse([]string{"-run", "a", "-run", "b,c"}))
	assert.Equal(t, List{"a", "b,c"}, list)
	assert.Equal(t, "a,b,c", list.String())
}
