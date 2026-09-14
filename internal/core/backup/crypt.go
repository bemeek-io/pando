package backup

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"

	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/secret"
)

// Bundle encryption (R-213).
//
// The passphrase is supplied at backup time and never stored. That is not a
// preference: bundling the host's own encryption key with the encrypted
// database would make the bundle plaintext for anyone holding it — every secret
// for every app in one file — and a restore onto a fresh machine cannot unwrap
// keys held by the machine that died.
//
// Format, all big-endian:
//
//	"PANDOBK1"        8 bytes, magic and version
//	salt             16 bytes
//	time, memory      4 + 4 bytes, argon2id parameters
//	parallelism       1 byte
//	then chunks:      4-byte ciphertext length, then the ciphertext
//
// The header is authenticated as additional data on every chunk, so the KDF
// parameters cannot be edited down to something cheap to brute-force. Chunk
// nonces are a counter, which is safe because the salt is fresh per bundle and
// therefore so is the key.
//
// The last chunk is marked in its own nonce. Without that marker, truncating a
// bundle after any chunk boundary produces a shorter bundle that decrypts
// perfectly — the exact "looks whole, is not" failure R-215 exists to catch,
// and catching it here is better than catching it against the manifest, because
// here it costs nothing.

const (
	magic     = "PANDOBK1"
	saltLen   = 16
	headerLen = len(magic) + saltLen + 4 + 4 + 1

	// chunkSize is the plaintext per chunk. 1 MiB keeps the per-chunk overhead
	// (16 bytes of tag, 4 of length) under 0.002% while bounding how much has
	// to be held in memory at once — a DR bundle is a pg_dump plus every app
	// volume and does not fit anywhere.
	chunkSize = 1 << 20

	// KDF cost. Deliberately higher than the interactive login in
	// internal/hash: unlocking a bundle happens once, in a disaster, and an
	// attacker holding a stolen bundle has unlimited time. Encoded in the
	// header so raising these later does not orphan existing bundles.
	kdfTime        = 4
	kdfMemory      = 256 * 1024 // 256 MiB
	kdfParallelism = 4
)

// ErrPassphrase is returned when a bundle will not decrypt.
//
// One error for a wrong passphrase and for a corrupt header, deliberately. An
// attacker learning *which* of those it was learns whether they have a real
// bundle, and R-214's cost is already high enough without leaking that.
var ErrPassphrase = errors.New("the passphrase does not open this bundle")

// Encrypt streams src to dst, encrypted under passphrase.
func Encrypt(dst io.Writer, src io.Reader, passphrase secret.Value) error {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return errs.Wrap(errs.Internal, "Pando could not start the backup.", err)
	}

	header := buildHeader(salt, kdfTime, kdfMemory, kdfParallelism)
	if _, err := dst.Write(header); err != nil {
		return errs.Wrap(errs.Internal, "Pando could not write the backup.", err)
	}

	aead, err := newAEAD(passphrase, salt, kdfTime, kdfMemory, kdfParallelism)
	if err != nil {
		return err
	}

	plain := make([]byte, chunkSize)
	var counter uint64
	for {
		n, readErr := io.ReadFull(src, plain)
		last := errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF)
		if readErr != nil && !last {
			return errs.Wrap(errs.Internal, "Pando could not read the backup contents.", readErr)
		}

		// A zero-length final chunk is still written, so that an empty payload
		// and a truncated one remain distinguishable.
		if n > 0 || last {
			sealed := aead.Seal(nil, nonce(counter, last), plain[:n], header)
			if err := writeChunk(dst, sealed); err != nil {
				return err
			}
			counter++
		}
		if last {
			return nil
		}
	}
}

// Decrypt streams an encrypted bundle from src to dst.
func Decrypt(dst io.Writer, src io.Reader, passphrase secret.Value) error {
	header := make([]byte, headerLen)
	if _, err := io.ReadFull(src, header); err != nil {
		return ErrPassphrase
	}
	if string(header[:len(magic)]) != magic {
		return ErrPassphrase
	}

	salt := header[len(magic) : len(magic)+saltLen]
	rest := header[len(magic)+saltLen:]
	t := binary.BigEndian.Uint32(rest[0:4])
	m := binary.BigEndian.Uint32(rest[4:8])
	p := rest[8]

	// Refuse parameters that would make unlocking trivial or impossible. An
	// attacker who can edit the header cannot forge a chunk — the header is
	// additional data — but they can make Pando try to allocate 4 TiB.
	if t == 0 || t > 16 || m < 8*1024 || m > 2*1024*1024 || p == 0 || p > 16 {
		return ErrPassphrase
	}

	aead, err := newAEAD(passphrase, salt, t, m, p)
	if err != nil {
		return err
	}

	var counter uint64
	for {
		sealed, err := readChunk(src)
		if err != nil {
			return err
		}

		// Try the not-last nonce, then the last-chunk one. Exactly one can
		// authenticate, because the nonce is part of what the tag covers.
		plain, openErr := aead.Open(nil, nonce(counter, false), sealed, header)
		if openErr != nil {
			plain, openErr = aead.Open(nil, nonce(counter, true), sealed, header)
			if openErr != nil {
				return ErrPassphrase
			}
			if _, err := dst.Write(plain); err != nil {
				return errs.Wrap(errs.Internal, "Pando could not write the restored data.", err)
			}

			// A last chunk must be the last thing in the file. Trailing bytes
			// mean the bundle was assembled by something other than Pando.
			if _, err := src.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
				return ErrPassphrase
			}
			return nil
		}

		if _, err := dst.Write(plain); err != nil {
			return errs.Wrap(errs.Internal, "Pando could not write the restored data.", err)
		}
		counter++
	}
}

func buildHeader(salt []byte, t, m uint32, p uint8) []byte {
	h := make([]byte, 0, headerLen)
	h = append(h, magic...)
	h = append(h, salt...)
	h = binary.BigEndian.AppendUint32(h, t)
	h = binary.BigEndian.AppendUint32(h, m)
	return append(h, p)
}

func newAEAD(passphrase secret.Value, salt []byte, t, m uint32, p uint8) (interface {
	Seal(dst, nonce, plaintext, additionalData []byte) []byte
	Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error)
}, error) {
	key := argon2.IDKey([]byte(passphrase.Reveal()), salt, t, m, p, chacha20poly1305.KeySize)
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Pando could not prepare the backup cipher.", err)
	}
	return aead, nil
}

// nonce is the chunk counter, with the final byte marking the last chunk.
func nonce(counter uint64, last bool) []byte {
	n := make([]byte, chacha20poly1305.NonceSize)
	binary.BigEndian.PutUint64(n[0:8], counter)
	if last {
		n[chacha20poly1305.NonceSize-1] = 1
	}
	return n
}

func writeChunk(dst io.Writer, sealed []byte) error {
	var length [4]byte
	// G115: sealed is one chunk plus the AEAD tag, bounded by chunkSize well
	// below 2^32. The read side rejects any length above that bound.
	binary.BigEndian.PutUint32(length[:], uint32(len(sealed))) //nolint:gosec
	if _, err := dst.Write(length[:]); err != nil {
		return errs.Wrap(errs.Internal, "Pando could not write the backup.", err)
	}
	if _, err := dst.Write(sealed); err != nil {
		return errs.Wrap(errs.Internal, "Pando could not write the backup.", err)
	}
	return nil
}

func readChunk(src io.Reader) ([]byte, error) {
	var length [4]byte
	if _, err := io.ReadFull(src, length[:]); err != nil {
		// Including EOF: a bundle that ends without a last-chunk marker is
		// truncated, which is the case this whole construction exists to make
		// detectable.
		return nil, ErrPassphrase
	}
	n := binary.BigEndian.Uint32(length[:])
	if n == 0 || n > chunkSize+uint32(chacha20poly1305.Overhead) {
		return nil, ErrPassphrase
	}
	sealed := make([]byte, n)
	if _, err := io.ReadFull(src, sealed); err != nil {
		return nil, ErrPassphrase
	}
	return sealed, nil
}
