// Package hash derives and verifies password and token hashes with argon2id.
//
// Used for both local user passwords (R-042) and token secrets, which are shown
// once and stored hashed (R-063). Kept out of core so adapters may use it
// without importing anything the R-027 boundary forbids.
package hash

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/bemeek-io/pando/internal/secret"
)

// Parameters. argon2id, tuned for an interactive login on a single host.
// Encoded into every hash, so raising them later does not invalidate existing
// credentials — a hash carries the parameters it was made with.
const (
	timeCost    = 3
	memoryCost  = 64 * 1024 // 64 MiB
	parallelism = 2
	saltLength  = 16
	keyLength   = 32
)

// New derives an encoded hash of v in the standard argon2 string format.
func New(v secret.Value) (string, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generating salt: %w", err)
	}

	key := argon2.IDKey([]byte(v.Reveal()), salt, timeCost, memoryCost, parallelism, keyLength)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memoryCost, timeCost, parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// Verify reports whether v matches the encoded hash.
//
// The comparison is constant time. A malformed hash returns false with an
// error rather than panicking, because a corrupted row must deny access, not
// crash the login path.
func Verify(v secret.Value, encoded string) (bool, error) {
	params, salt, key, err := decode(encoded)
	if err != nil {
		return false, err
	}

	candidate := argon2.IDKey([]byte(v.Reveal()), salt,
		params.time, params.memory, params.parallelism, uint32(len(key)))

	return subtle.ConstantTimeCompare(key, candidate) == 1, nil
}

type params struct {
	memory      uint32
	time        uint32
	parallelism uint8
}

func decode(encoded string) (params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return params{}, nil, nil, fmt.Errorf("malformed argon2id hash")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return params{}, nil, nil, fmt.Errorf("malformed argon2id version: %w", err)
	}
	if version != argon2.Version {
		return params{}, nil, nil, fmt.Errorf("unsupported argon2 version %d", version)
	}

	var p params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.parallelism); err != nil {
		return params{}, nil, nil, fmt.Errorf("malformed argon2id parameters: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return params{}, nil, nil, fmt.Errorf("malformed argon2id salt: %w", err)
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return params{}, nil, nil, fmt.Errorf("malformed argon2id key: %w", err)
	}
	return p, salt, key, nil
}
