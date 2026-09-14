package backup_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/bemeek-io/pando/internal/core/backup"
)

// Header layout, from the format comment in crypt.go: 8 bytes of magic, 16 of
// salt, then 4 + 4 + 1 of argon2id parameters.
const (
	magicLen    = 8
	saltLen     = 16
	paramOffset = magicLen + saltLen
	headerLen   = paramOffset + 4 + 4 + 1
)

// FuzzDecryptEnvelope feeds arbitrary bytes to the bundle decryptor.
//
// Decrypt parses a length-prefixed chunk format from a file Pando did not
// necessarily write — a restore reads whatever the operator points it at, and
// "point restore at a file" is the whole interface. Every field, including each
// chunk length, is attacker-controlled before any authentication tag has been
// checked.
//
// The property: a bundle Pando did not seal produces an error, never a panic
// and never plaintext. Sequence D's guarantee is that a bad bundle is rejected
// with the target install completely untouched, and a panic partway through
// parsing is not "untouched".
//
// The KDF parameters are overwritten with the cheapest values Decrypt accepts
// before each call. Left to the fuzzer they are the whole cost of the run: the
// header may legitimately ask for 2 GiB and sixteen passes, so every execution
// spends seconds inside argon2id and the chunk parser — the part with the
// interesting bugs in it — is never reached twice. Decrypt's own bounds on
// those fields are covered by TestR213 cases rather than here.
func FuzzDecryptEnvelope(f *testing.F) {
	var sealed bytes.Buffer
	if err := backup.Encrypt(&sealed, bytes.NewReader([]byte("some bundle bytes")),
		pass("correct passphrase")); err != nil {
		f.Fatal(err)
	}
	good := sealed.Bytes()

	f.Add(good)
	f.Add([]byte(nil))
	f.Add([]byte{0})
	f.Add(good[:headerLen])               // header and nothing else
	f.Add(good[:len(good)-1])             // truncated mid-tag
	f.Add(append(bytes.Clone(good), 0))   // trailing byte after the last chunk
	f.Add(bytes.Repeat([]byte{0xff}, 64)) // every length field at maximum

	f.Fuzz(func(t *testing.T, sealed []byte) {
		if len(sealed) > 64<<10 {
			t.Skip()
		}
		sealed = bytes.Clone(sealed)
		cheapenKDF(sealed)

		var out bytes.Buffer
		err := backup.Decrypt(&out, bytes.NewReader(sealed),
			pass("a passphrase the fuzzer does not know"))
		if err == nil {
			t.Fatalf("decrypted %d bytes with the wrong passphrase", len(sealed))
		}
	})
}

// cheapenKDF rewrites the header's argon2id parameters in place to the cheapest
// values Decrypt will accept, leaving the magic, the salt and every chunk to
// the fuzzer. A buffer too short to hold a header is left alone — Decrypt has
// to reject that on its own.
func cheapenKDF(b []byte) {
	if len(b) < headerLen {
		return
	}
	binary.BigEndian.PutUint32(b[paramOffset:], 1)        // time
	binary.BigEndian.PutUint32(b[paramOffset+4:], 8*1024) // memory, the floor
	b[paramOffset+8] = 1                                  // parallelism
}
