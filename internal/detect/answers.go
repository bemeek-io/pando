package detect

import (
	"strconv"
	"strings"

	"github.com/bemeek-io/pando/internal/core/spec"
)

// Question keys detection produces. Answers are matched back by these, so they
// are a small fixed vocabulary rather than free-form strings.
const (
	KeyPrimaryPort       = "primary_port"
	KeyPrimaryService    = "primary_service"
	KeyStartCommand      = "start_command"
	KeyDockerfilePath    = "dockerfile_path"
	KeyDeployableProject = "deployable_project"
	KeyStaticSource      = "static_source"
	KeyBuildStrategy     = "build_strategy"
	KeyBuildMethod       = "build_method"
)

// WithAnswers folds a user's answers into the draft spec.
//
// An answer is worth more than anything Pando worked out for itself, so it
// overwrites rather than merges — and a port supplied by a person is recorded
// with Source "user", which is what the review UI shows beside it (design 01
// §2.3). Where the value came from is part of the value.
func (p Proposal) WithAnswers(answers map[string]string) spec.AppSpec {
	// The tie-break is resolved first, because it chooses which reading of the
	// repository the rest of the answers apply to.
	out := p.chosen(answers)

	// Copy the workload slice: a proposal is read from storage and may be
	// applied more than once, and mutating it in place would make the second
	// application see the first one's results.
	out.Workloads = append([]spec.Workload(nil), out.Workloads...)

	for key, value := range answers {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		switch key {
		case KeyPrimaryPort:
			out = withPort(out, value)
		case KeyPrimaryService:
			out = withPrimaryService(out, value)
		case KeyStartCommand:
			out = withStartCommand(out, value)
		case KeyDockerfilePath:
			out.Build.Dockerfile = value
			out.Build.Strategy = spec.BuildDockerfile
		case KeyDeployableProject:
			// The answer names a directory inside the repository, which is the
			// same thing a subdir is. Detection has to run again against it —
			// the proposal describes the repository root, and nothing about it
			// applies to a project one level down.
			out.Source.Subdir = value
		case KeyStaticSource:
			out.Build.Strategy = spec.BuildStatic
			if value == "run-build" {
				out.Build.Strategy = spec.BuildBuildpack
			}
		case KeyBuildStrategy, KeyBuildMethod:
			// Applied by chosen() above, which swapped in that candidate's
			// whole draft. Setting the strategy here is what used to produce a
			// spec describing one detector's reading under another's name.
		}
	}
	return out
}

// chosen returns the reading the answers select, defaulting to the winner's.
//
// The tie-break offers the two close candidates' own strategies (see tieBreak),
// so an answer naming one of them means "that detector read this repository
// correctly" — which is a different draft, not a different label on this one.
//
// One honest limitation: the winner's draft has been through the trial run and
// a runner-up's has not, so adopting one gives up whatever the trial
// discovered — a port observed rather than declared, mostly. Re-running the
// trial here would be better and is a larger change; producing a coherent spec
// instead of a corrupt one is the part that could not wait.
func (p Proposal) chosen(answers map[string]string) spec.AppSpec {
	want := spec.BuildStrategy(strings.TrimSpace(answers[KeyBuildStrategy]))
	if want == "" {
		want = spec.BuildStrategy(strings.TrimSpace(answers[KeyBuildMethod]))
	}
	if want == "" || want == p.Winner.Strategy {
		return p.DraftSpec
	}

	for _, c := range p.RunnersUp {
		if c.Strategy != want {
			continue
		}
		// A candidate that recognized the repository and could not import it —
		// a compose file using a construct that cannot cross the boundary
		// (R-099) — has no workloads. Adopting it would replace a working
		// proposal with an empty one.
		if len(c.Draft.Workloads) == 0 {
			return p.DraftSpec
		}
		return Assemble(p.DraftSpec.AppID, p.DraftSpec.Source, c.Draft)
	}

	// An answer naming something that did not bid is not a choice between
	// readings. Ignored rather than invented: stamping it on the winner is
	// exactly the bug this function exists to remove.
	return p.DraftSpec
}

func withPort(s spec.AppSpec, value string) spec.AppSpec {
	number, err := strconv.Atoi(value)
	if err != nil || number <= 0 || number > 65535 {
		return s
	}
	for i, w := range s.Workloads {
		if !w.Primary {
			continue
		}
		s.Workloads[i].Ports = []spec.Port{{
			Number: number, Protocol: "http", Source: spec.PortUser,
		}}
		break
	}
	return s
}

// withPrimaryService moves Primary to the named workload.
//
// Exactly one workload is primary — the app has one canonical endpoint — so
// this clears the flag everywhere before setting it, rather than assuming the
// caller's draft had it in one place.
func withPrimaryService(s spec.AppSpec, name string) spec.AppSpec {
	var found bool
	for i := range s.Workloads {
		if s.Workloads[i].Name == name {
			found = true
		}
	}
	if !found {
		return s
	}
	for i := range s.Workloads {
		isPrimary := s.Workloads[i].Name == name
		s.Workloads[i].Primary = isPrimary
		s.Workloads[i].Exposed = isPrimary
	}
	return s
}

func withStartCommand(s spec.AppSpec, command string) spec.AppSpec {
	for i, w := range s.Workloads {
		if !w.Primary {
			continue
		}
		// Kept as a shell command rather than split into argv. Splitting on
		// whitespace breaks the first quoted argument anyone writes, and the
		// person answering this question wrote a command line, not an argv.
		s.Workloads[i].Command = []string{"sh", "-c", command}
		break
	}
	return s
}
