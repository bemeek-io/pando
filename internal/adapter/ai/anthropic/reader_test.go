package anthropic

import (
	"errors"
	"io"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/secret"
)

func secretOf(s string) secret.Value { return secret.New(s) }

type files map[string]string

func (m files) Open(name string) (io.ReadCloser, error) {
	content, ok := m[path.Clean(name)]
	if !ok {
		return nil, io.EOF
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func (m files) Stat(name string) (api.FileInfo, error) {
	if content, ok := m[path.Clean(name)]; ok {
		return api.FileInfo{Name: name, Size: int64(len(content))}, nil
	}
	return api.FileInfo{}, io.EOF
}

func (m files) Glob(string) ([]string, error) { return nil, nil }

// TestR339_TheFileBudgetIsEnforced asserts R-339.
func TestR339_TheFileBudgetIsEnforced(t *testing.T) {
	r := newReader(files{"a": "1", "b": "2", "c": "3"}, 2, 1<<20)

	_, err := r.open("a")
	require.NoError(t, err)
	_, err = r.open("b")
	require.NoError(t, err)

	_, err = r.open("c")
	require.Error(t, err)
	require.True(t, errors.Is(err, errBudget),
		"a spent budget is reported as such, so the loop can ask for findings rather than fail")
	require.ElementsMatch(t, []string{"a", "b"}, r.files())
}

// TestR339_RereadingAFileIsFree asserts R-339.
//
// A model asking for a file twice lost track; it is not spending a second file
// of budget, and charging it would end screenings early for no gain.
func TestR339_RereadingAFileIsFree(t *testing.T) {
	r := newReader(files{"a": "1", "b": "2"}, 1, 1<<20)

	_, err := r.open("a")
	require.NoError(t, err)
	_, err = r.open("a")
	require.NoError(t, err, "the same file again")

	_, err = r.open("b")
	require.Error(t, err, "a different one is still over the limit")
	require.Len(t, r.files(), 1)
}

// TestR339_TheByteBudgetIsEnforced asserts R-339.
func TestR339_TheByteBudgetIsEnforced(t *testing.T) {
	r := newReader(files{"big": strings.Repeat("x", 4096), "next": "y"}, 10, 512)

	body, err := r.open("big")
	require.NoError(t, err)
	require.Contains(t, body, "truncated", "one enormous file cannot spend the whole budget silently")
	require.LessOrEqual(t, len(body), 512+64)

	_, err = r.open("next")
	require.True(t, errors.Is(err, errBudget))
}

// TestR020_AReadCannotLeaveTheCheckout asserts R-020.
//
// The SourceView is already rooted at the checkout, so this is the second lock
// on a bolted door — and it stays, because "the other layer handles it" is how
// both layers end up not handling it.
func TestR020_AReadCannotLeaveTheCheckout(t *testing.T) {
	r := newReader(files{"a": "1"}, 10, 1<<20)

	for _, bad := range []string{"/etc/passwd", "../../../etc/passwd", "..", ""} {
		_, err := r.open(bad)
		require.Error(t, err, bad)
		require.False(t, errors.Is(err, errBudget), bad)
	}
	require.Empty(t, r.files(), "nothing refused was recorded as read")
}

// TestR337_WhatWasReadIsRecordedInOrder asserts R-337.
//
// Not a security boundary — an adapter that lied has already read the file. It
// is the operator's record of what was sent, which is the thing somebody wants
// after the fact and cannot reconstruct.
func TestR337_WhatWasReadIsRecordedInOrder(t *testing.T) {
	r := newReader(files{"a": "1", "b": "2", "c": "3"}, 10, 1<<20)

	for _, name := range []string{"b", "a", "b", "c"} {
		_, err := r.open(name)
		require.NoError(t, err)
	}
	require.Equal(t, []string{"b", "a", "c"}, r.files(), "each distinct file once, in read order")
}

// TestO20_TheKeyResolvesFromEncryptedStorageOrTheEnvironment asserts O-20's
// resolution: a key comes from the credential core decrypted, or from the
// environment, and from nowhere else.
func TestO20_TheKeyResolvesFromEncryptedStorageOrTheEnvironment(t *testing.T) {
	env := map[string]string{"PANDO_AI_KEY": "from-named", "ANTHROPIC_API_KEY": "from-default"}
	getenv := func(k string) string { return env[k] }

	stored := Config{Credentials: Credentials{APIKey: secretOf("stored")}, APIKeyEnv: "PANDO_AI_KEY"}
	require.Equal(t, "stored", resolveKey(stored, getenv).Reveal(),
		"the stored credential wins, because somebody set it deliberately")
	require.Equal(t, "from-named", resolveKey(Config{APIKeyEnv: "PANDO_AI_KEY"}, getenv).Reveal())
	require.Equal(t, "from-default", resolveKey(Config{}, getenv).Reveal(),
		"the SDK's own variable when nothing is named")
	require.True(t, resolveKey(Config{APIKeyEnv: "UNSET"}, getenv).IsZero(),
		"a named variable that is empty is not silently replaced by the default one")
}
