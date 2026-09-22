package hash

import (
	"crypto/rand"
	"math/big"
	"strings"

	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/secret"
)

// The generator's alphabet, by class. The symbols leave out quotes, backslash,
// backtick and space: a password handed over in a chat message or pasted into
// a shell should survive both unchanged.
const (
	lower   = "abcdefghijklmnopqrstuvwxyz"
	upper   = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	digits  = "0123456789"
	symbols = "!@#$%^&*-_=+?~.,:;"
)

// GeneratedMin and GeneratedMax bound a generated password's length. The
// length itself is random within them, so the length says nothing either.
const (
	GeneratedMin = 18
	GeneratedMax = 22
)

// Generate returns a password nobody chose: 18 to 22 characters from upper and
// lower case letters, digits and symbols, with at least one of each.
//
// The one generator in Pando. First run's reset command, an administrator
// creating an account and an administrator resetting one all come here, so they
// cannot drift apart in length or alphabet — two generators eventually would,
// and the weaker one would be the one nobody looked at.
//
// "At least one of each" is met by drawing again rather than by placing one of
// each at a chosen position, which would make those positions predictable. At
// 18+ characters from 80 symbols a draw missing a class is rare, so this almost
// never loops.
func Generate() (secret.Value, error) {
	all := lower + upper + digits + symbols
	for {
		n, err := randomInt(GeneratedMax - GeneratedMin + 1)
		if err != nil {
			return secret.Value{}, err
		}
		length := GeneratedMin + n

		var b strings.Builder
		for range length {
			i, err := randomInt(len(all))
			if err != nil {
				return secret.Value{}, err
			}
			b.WriteByte(all[i])
		}
		s := b.String()
		if strings.ContainsAny(s, lower) && strings.ContainsAny(s, upper) &&
			strings.ContainsAny(s, digits) && strings.ContainsAny(s, symbols) {
			return secret.New(s), nil
		}
	}
}

func randomInt(n int) (int, error) {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, errs.Wrap(errs.Internal, "Could not generate a password.", err)
	}
	return int(v.Int64()), nil
}
