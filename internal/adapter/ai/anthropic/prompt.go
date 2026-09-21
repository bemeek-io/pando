package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
)

// systemPrompt states the job and the rules core will enforce anyway.
//
// Stating them twice is not redundancy. Core refuses an amendment that breaks
// one of these (design 09 §3), so a model that has not been told is a model
// whose work is thrown away — the refusal is correct and the screening is
// wasted. Telling it here is what turns a refusal into a rarity.
const systemPrompt = `You are screening a deployment plan for Pando, a self-hosted platform that builds
and runs applications from their source repositories.

Pando has already analyzed this repository with deterministic detectors and produced a plan. Your job
is not to produce a plan of your own. It is to read the repository and find what those detectors
missed or got wrong, so that this application starts successfully on the first deploy without anyone
having to be asked a question.

The most valuable things you can find, roughly in order:

- A start command that is wrong, missing, or starts a development server where a production one is
  meant.
- An application that binds to 127.0.0.1 or localhost and will therefore be unreachable behind a
  proxy. The fix is usually an environment variable such as HOST=0.0.0.0.
- An environment variable the application exits without, which is documented in the README, an
  .env.example, or a config file, and which is not in the plan.
- A declared dependency — a database, a cache — that is referenced somewhere the detectors did not
  look, such as a framework config file rather than an .env.example.
- In a monorepo, the wrong directory or the wrong Dockerfile being built.
- A directory the application writes to that is not backed by a volume, so its contents are lost on
  the next deploy.
- A question Pando could not answer that the repository in fact answers.

Rules that are enforced, not advisory. An amendment breaking one of these is discarded:

1. Every amendment must cite at least one repository path in "evidence", and those paths are checked
   to exist. Cite files you actually read. An amendment resting on what you know about a framework
   rather than on what this repository says will be thrown away.
2. You cannot change isolation, security policy, resource limits, egress, routing, which adapters are
   used, or any secret value. There is no amendment that expresses those, and asking for one in prose
   accomplishes nothing.
3. If Pando observed something by running the application — a port it bound, a directory it wrote —
   that observation wins. Do not contradict it. You may supply what was not observed.
4. Do not invent a dependency the repository never mentions. If an application needs a database and
   nothing in the repository says so, the correct outcome is that it fails and a person is shown the
   log. That is by design.
5. Mark a dependency "required" only if the trial run actually crashed because of it. A required
   dependency that is unfilled blocks the deploy entirely, which is worse than an application that
   starts and reports a problem.

Write every "reason" so that someone who cannot see the repository understands it, in plain language,
with no apology, no "Error:" prefix and no exclamation mark. "The start command runs the Vite dev
server; package.json defines a start script that serves the built output" is right. "Fixed the
command" is not.

Read what you need, then call submit_findings exactly once. Submitting no amendments is a good
outcome when the plan is already correct — do not invent work.`

// userPrompt is the proposal, rendered.
func userPrompt(req api.ScreenRequest) string {
	var b strings.Builder

	b.WriteString("Here is the plan Pando produced for this repository.\n\n")
	b.WriteString("## How Pando read this repository\n\n")
	if len(req.Evidence) == 0 {
		b.WriteString("(no evidence recorded)\n")
	}
	for _, e := range req.Evidence {
		fmt.Fprintf(&b, "- %s\n", e)
	}

	b.WriteString("\n## The plan\n\n```json\n")
	b.WriteString(planJSON(req.Spec))
	b.WriteString("\n```\n")

	b.WriteString("\n## The trial run\n\n")
	b.WriteString(trialText(req.Trial))

	if len(req.Questions) > 0 {
		b.WriteString("\n## Questions Pando could not answer\n\n")
		b.WriteString("Answer one with an `answer_question` amendment using its key, if and only if " +
			"the repository actually answers it.\n\n")
		for _, q := range req.Questions {
			fmt.Fprintf(&b, "- **%s** (%s): %s\n", q.Key, q.Kind, q.Prompt)
			if len(q.Options) > 0 {
				fmt.Fprintf(&b, "  Valid answers: %s\n", strings.Join(q.Options, ", "))
			}
		}
	}

	fmt.Fprintf(&b, "\nYou may read up to %d files and %d bytes. Start by listing the repository root.\n",
		req.Budget.MaxFiles, req.Budget.MaxBytes)
	return b.String()
}

// planJSON renders the parts of the spec a screener can act on.
//
// Not the whole spec. Routing, runtime, resources, egress and retention are the
// install's answers rather than the repository's (R-104), and nothing in the
// amendment set can change them — showing a model fields it cannot touch
// invites amendments that can only be refused. Routing.Mode is the exception
// and it is included, because whether an app is served under a path prefix is
// something the repository has an opinion about.
func planJSON(s spec.AppSpec) string {
	type workload struct {
		Name       string            `json:"name"`
		Image      string            `json:"image,omitempty"`
		Command    []string          `json:"command,omitempty"`
		Entrypoint []string          `json:"entrypoint,omitempty"`
		WorkingDir string            `json:"working_dir,omitempty"`
		Env        map[string]string `json:"env,omitempty"`
		Ports      []spec.Port       `json:"ports,omitempty"`
		Mounts     []spec.Mount      `json:"mounts,omitempty"`
		Primary    bool              `json:"primary"`
	}

	view := struct {
		Build       spec.Build       `json:"build"`
		Workloads   []workload       `json:"workloads"`
		Volumes     []spec.Volume    `json:"volumes,omitempty"`
		Slots       []spec.Slot      `json:"slots,omitempty"`
		Health      spec.Health      `json:"health"`
		Warnings    []spec.Warning   `json:"warnings,omitempty"`
		RoutingMode spec.RoutingMode `json:"routing_mode"`
		Subdir      string           `json:"subdir,omitempty"`
	}{
		Build:       s.Build,
		Volumes:     s.Volumes,
		Slots:       s.Slots,
		Health:      s.Health,
		Warnings:    s.Warnings,
		RoutingMode: s.Routing.Mode,
		Subdir:      s.Source.Subdir,
	}

	for _, w := range s.Workloads {
		out := workload{
			Name: w.Name, Image: w.Image, Command: w.Command, Entrypoint: w.Entrypoint,
			WorkingDir: w.WorkingDir, Ports: w.Ports, Mounts: w.Mounts, Primary: w.Primary,
		}
		if len(w.Env) > 0 {
			out.Env = map[string]string{}
		}
		for _, e := range w.Env {
			switch {
			case e.Value != nil:
				out.Env[e.Key] = *e.Value
			case e.SlotRef != nil:
				out.Env[e.Key] = "(filled from the " + *e.SlotRef + " dependency)"
			case e.SecretRef != nil:
				// A spec holds no secret values (R-020), and this is where that
				// property earns its keep: the plan is safe to send because
				// there was never anything sensitive in it to withhold.
				out.Env[e.Key] = "(a secret, not shown)"
			}
		}
		view.Workloads = append(view.Workloads, out)
	}

	body, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(body)
}

func trialText(t api.TrialSummary) string {
	if !t.Ran {
		return "Pando did not run this application. Nothing here was observed, so anything you can " +
			"establish from the repository is worth having.\n"
	}

	var b strings.Builder
	if t.Crashed {
		b.WriteString("Pando started this application and it exited with an error. ")
	} else {
		b.WriteString("Pando started this application and it stayed up. ")
	}
	if len(t.ObservedPorts) > 0 {
		fmt.Fprintf(&b, "It was observed binding %v — that is settled and cannot be changed. ",
			t.ObservedPorts)
	} else {
		b.WriteString("It was not observed binding any port. ")
	}
	if len(t.ObservedWrites) > 0 {
		fmt.Fprintf(&b, "It wrote to %s, outside any declared volume. ",
			strings.Join(t.ObservedWrites, ", "))
	}

	if t.Log != "" {
		b.WriteString("\n\nIts output:\n\n```\n")
		b.WriteString(tail(t.Log, maxLogBytes))
		b.WriteString("\n```\n")
	}
	return b.String()
}

const maxLogBytes = 16 << 10

// tail keeps the end of a log, which is where the reason is.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[len(s)-n:]
	if i := strings.IndexByte(cut, '\n'); i >= 0 && i < len(cut)-1 {
		cut = cut[i+1:]
	}
	return "[earlier output omitted]\n" + cut
}
