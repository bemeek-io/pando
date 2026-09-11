package secret_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/bemeek-io/pando/internal/secret"
)

const canary = "hunter2-THE-ACTUAL-SECRET"

// TestR194_ValueNeverRendersThroughAnyFormattingVerb asserts R-194.
//
// The verbs are enumerated rather than sampled because each reaches the value by
// a different mechanism: %s and %v go through Stringer, %#v through GoStringer,
// and the rest would hit the struct directly if Formatter were missing.
func TestR194_ValueNeverRendersThroughAnyFormattingVerb(t *testing.T) {
	v := secret.New(canary)

	for _, verb := range []string{"%s", "%v", "%+v", "%#v", "%q", "%x", "%X", "%d", "%t"} {
		got := fmt.Sprintf(verb, v)
		require.NotContains(t, got, canary, "verb %s leaked the secret", verb)
		require.Equal(t, secret.Redacted, got, "verb %s should render the redaction string", verb)
	}
}

// TestR194_ValueNeverRendersWhenNested asserts R-194 for the realistic case: a
// secret is rarely formatted directly, it is a field on something being logged.
func TestR194_ValueNeverRendersWhenNested(t *testing.T) {
	type config struct {
		Host     string
		Password secret.Value
	}
	c := config{Host: "db.internal", Password: secret.New(canary)}

	for _, verb := range []string{"%v", "%+v", "%#v"} {
		require.NotContains(t, fmt.Sprintf(verb, c), canary, "verb %s leaked a nested secret", verb)
	}

	b, err := json.Marshal(c)
	require.NoError(t, err)
	require.NotContains(t, string(b), canary)
	require.Contains(t, string(b), secret.Redacted)
}

// TestR194_ValueNeverReachesALogLine asserts R-194 against zap specifically,
// which is the path the requirement is actually about.
func TestR194_ValueNeverReachesALogLine(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	log := zap.New(core)

	v := secret.New(canary)
	log.Info("connecting",
		zap.Any("password", v),
		zap.Stringer("password_stringer", v),
		zap.String("password_string", v.String()),
		zap.Int("password_len", v.Len()),
	)

	require.Equal(t, 1, logs.Len())
	entry := logs.All()[0]

	encoded, err := json.Marshal(entry.ContextMap())
	require.NoError(t, err)
	require.NotContains(t, string(encoded), canary, "a secret reached a log line")
}

// TestR194_RevealIsTheOnlyWayOut asserts that the protection is not merely
// cosmetic: the value is retrievable, but only deliberately.
func TestR194_RevealIsTheOnlyWayOut(t *testing.T) {
	v := secret.New(canary)
	require.Equal(t, canary, v.Reveal())
	require.Equal(t, len(canary), v.Len())
	require.False(t, v.IsZero())
	require.True(t, secret.Value{}.IsZero())
}

// TestR194_RoundTrippingARedactedPayloadDoesNotSetTheSecret asserts that
// decoding output produced by this type cannot silently overwrite a real secret
// with the literal redaction string — the obvious bug once redaction works.
func TestR194_RoundTrippingARedactedPayloadDoesNotSetTheSecret(t *testing.T) {
	encoded, err := json.Marshal(secret.New(canary))
	require.NoError(t, err)

	var decoded secret.Value
	require.NoError(t, json.Unmarshal(encoded, &decoded))

	require.True(t, decoded.IsZero(), "decoding a redacted value should yield empty, not %q", decoded.Reveal())
	require.NotEqual(t, secret.Redacted, decoded.Reveal())
}

// TestR194_UnmarshalAcceptsARealSecret asserts the decode path still works for
// genuine input.
func TestR194_UnmarshalAcceptsARealSecret(t *testing.T) {
	var v secret.Value
	require.NoError(t, json.Unmarshal([]byte(`"s3cret"`), &v))
	require.Equal(t, "s3cret", v.Reveal())

	require.Error(t, json.Unmarshal([]byte(`{"not":"a string"}`), &v))
}

// TestR194_ErrorStringsDoNotLeak asserts the error-envelope path, since a
// secret formatted into an error message is how it would reach an API response.
func TestR194_ErrorStringsDoNotLeak(t *testing.T) {
	v := secret.New(canary)
	err := fmt.Errorf("failed to connect with credential %v: %w", v, fmt.Errorf("timeout"))
	require.NotContains(t, err.Error(), canary)
	require.True(t, strings.Contains(err.Error(), secret.Redacted))
}
