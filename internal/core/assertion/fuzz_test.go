package assertion_test

import (
	"strings"
	"testing"

	"github.com/bemeek-io/pando/internal/core/assertion"
)

// FuzzVerifyToken feeds arbitrary strings to the assertion verifier.
//
// This is the highest-value fuzz target in the repository. An assertion is the
// only thing an app can trust about who is calling (R-053), so Verify is the
// function standing between "Pando says this is Alice" and "a client typed
// Alice into a header". It parses attacker-controlled input by definition: the
// token arrives in a request header.
//
// Two properties, and the second is the one that matters:
//
//  1. Verify never panics. It runs inside the proxy, on the request path.
//  2. Verify never accepts a token this minter did not sign. A fuzzer-invented
//     string returning claims would be a complete authentication bypass — an
//     attacker could assert any `sub` against any `aud`.
func FuzzVerifyToken(f *testing.F) {
	m, err := assertion.NewMinter("https://pando.test", nil)
	if err != nil {
		f.Fatal(err)
	}

	valid, err := m.Mint(assertion.Claims{Sub: "usr_alice", Aud: "app_01HQ8"})
	if err != nil {
		f.Fatal(err)
	}

	f.Add(valid)
	f.Add("")
	f.Add(".")
	f.Add("..")
	f.Add("a.b.c")
	f.Add("a.b.c.d")
	// The classic JWT forgeries: alg=none, and a signature stripped off a
	// token that was otherwise real.
	f.Add("eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiJ1c3JfYWRtaW4ifQ.")
	f.Add(valid[:strings.LastIndex(valid, ".")+1])
	f.Add(strings.Repeat("A", 512))

	f.Fuzz(func(t *testing.T, token string) {
		claims, err := m.Verify(token)
		if err != nil {
			return
		}
		// Accepted. The only token this minter should ever accept is one it
		// minted, so anything reaching here that is not the seed is a forgery.
		if token != valid {
			t.Fatalf("verified a token the minter did not sign: %q -> sub=%q aud=%q",
				token, claims.Sub, claims.Aud)
		}
	})
}
