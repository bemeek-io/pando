package aikit

import (
	"errors"
	"io"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
)

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
	r := NewReader(files{"a": "1", "b": "2", "c": "3"}, 2, 1<<20)

	_, err := r.Open("a")
	require.NoError(t, err)
	_, err = r.Open("b")
	require.NoError(t, err)

	_, err = r.Open("c")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrBudget),
		"a spent budget is reported as such, so the loop can ask for findings rather than fail")
	require.ElementsMatch(t, []string{"a", "b"}, r.Files())
}

// TestR339_RereadingAFileIsFree asserts R-339.
//
// A model asking for a file twice lost track; it is not spending a second file
// of budget, and charging it would end screenings early for no gain.
func TestR339_RereadingAFileIsFree(t *testing.T) {
	r := NewReader(files{"a": "1", "b": "2"}, 1, 1<<20)

	_, err := r.Open("a")
	require.NoError(t, err)
	_, err = r.Open("a")
	require.NoError(t, err, "the same file again")

	_, err = r.Open("b")
	require.Error(t, err, "a different one is still over the limit")
	require.Len(t, r.Files(), 1)
}

// TestR339_TheByteBudgetIsEnforced asserts R-339.
func TestR339_TheByteBudgetIsEnforced(t *testing.T) {
	r := NewReader(files{"big": strings.Repeat("x", 4096), "next": "y"}, 10, 512)

	body, err := r.Open("big")
	require.NoError(t, err)
	require.Contains(t, body, "truncated", "one enormous file cannot spend the whole budget silently")
	require.LessOrEqual(t, len(body), 512+64)

	_, err = r.Open("next")
	require.True(t, errors.Is(err, ErrBudget))
}

// TestR020_AReadCannotLeaveTheCheckout asserts R-020.
//
// The SourceView is already rooted at the checkout, so this is the second lock
// on a bolted door — and it stays, because "the other layer handles it" is how
// both layers end up not handling it.
func TestR020_AReadCannotLeaveTheCheckout(t *testing.T) {
	r := NewReader(files{"a": "1"}, 10, 1<<20)

	for _, bad := range []string{"/etc/passwd", "../../../etc/passwd", "..", ""} {
		_, err := r.Open(bad)
		require.Error(t, err, bad)
		require.False(t, errors.Is(err, ErrBudget), bad)
	}
	require.Empty(t, r.Files(), "nothing refused was recorded as read")
}

// TestR337_WhatWasReadIsRecordedInOrder asserts R-337.
//
// Not a security boundary — an adapter that lied has already read the file. It
// is the operator's record of what was sent, which is the thing somebody wants
// after the fact and cannot reconstruct.
func TestR337_WhatWasReadIsRecordedInOrder(t *testing.T) {
	r := NewReader(files{"a": "1", "b": "2", "c": "3"}, 10, 1<<20)

	for _, name := range []string{"b", "a", "b", "c"} {
		_, err := r.Open(name)
		require.NoError(t, err)
	}
	require.Equal(t, []string{"b", "a", "c"}, r.Files(), "each distinct file once, in read order")
}
