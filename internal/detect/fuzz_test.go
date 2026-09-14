package detect_test

import (
	"testing"

	"github.com/bemeek-io/pando/internal/detect"
)

// FuzzImportCompose feeds arbitrary YAML to the compose importer.
//
// R-096 makes a compose file a complete answer rather than a hint, which means
// this function reads a file out of an arbitrary git repository and turns it
// into most of an app spec. The repository belongs to whoever asked Pando to
// deploy it, so the input is untrusted in the ordinary case, not the exotic one.
//
// The property is narrow on purpose: any input either produces a draft or an
// error, and never a panic. A malformed compose file must fail planning with
// PLAN_* or a validation error — the thing it must not do is take down the
// detection worker, because that is a denial of service anybody who can add an
// app can trigger.
func FuzzImportCompose(f *testing.F) {
	f.Add("services:\n  web:\n    image: nginx\n")
	f.Add("services:\n  web:\n    build: .\n    ports: ['8080:80']\n")
	f.Add("services:\n  web:\n    image: nginx\n    privileged: true\n")
	f.Add("services:\n  a:\n    image: x\n    depends_on: [b]\n  b:\n    image: y\n")
	f.Add("services:\n  web:\n    image: nginx\n    volumes: ['/host:/container']\n")
	f.Add("")
	f.Add("services:\n")
	f.Add("services: []\n")
	f.Add("services: nonsense\n")
	f.Add("!!binary |\n  aGVsbG8=\n")
	f.Add("a: &x [*x]\n") // a self-referencing anchor

	f.Fuzz(func(t *testing.T, body string) {
		if len(body) > 64<<10 {
			t.Skip()
		}
		// The return values do not matter — either outcome is correct for
		// arbitrary input. Reaching the end of the call is the assertion.
		_, _ = detect.ImportCompose(memSource{"compose.yml": body}, "compose.yml")
	})
}
