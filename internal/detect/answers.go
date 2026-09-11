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
	out := p.DraftSpec

	// Copy the workload slice: a proposal is read from storage and may be
	// applied more than once, and mutating it in place would make the second
	// application see the first one's results.
	out.Workloads = append([]spec.Workload(nil), p.DraftSpec.Workloads...)

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
			out.Build.Strategy = spec.BuildStrategy(value)
		}
	}
	return out
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
