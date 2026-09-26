package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
)

// systemPrompt returns the job for fn, and the rules core will enforce anyway.
//
// Stating the rules twice is not redundancy. Core refuses an amendment that
// breaks one of these (design 10 §3), so a model that has not been told is a
// model whose work is thrown away — the refusal is correct and the call is
// wasted. Telling it here is what turns a refusal into a rarity.
func systemPrompt(fn api.AIFunction) string {
	switch fn {
	case api.AIFunctionAnswerQuestions:
		return answerPrompt + "\n\n" + rulesPrompt + "\n\n" + answerClosing
	case api.AIFunctionRevisePlan:
		return revisePrompt + "\n\n" + rulesPrompt + "\n\n" + reviseClosing
	default:
		return repairPrompt + "\n\n" + rulesPrompt + "\n\n" + repairClosing
	}
}

const revisePrompt = `You are helping a person review a deployment plan for Pando, a self-hosted platform that builds
and runs applications from their source repositories.

Pando analyzed this repository and produced a plan. The person reviewing it has told you something
about it — that a variable is missing, that the app serves on a different port, that it needs a
database the plan left out. Your job is to check what they said against the repository and, where the
repository supports it, change the plan with the amendments below. They know their app; the repository
is still the evidence. If what they said is true of the repository, make the change and cite the files
that show it. If the repository contradicts it or says nothing either way, change nothing and say so
plainly in your reply — they can still set it themselves in the plan's variables and settings.

If the message is not about the plan at all — a joke, a greeting, a question about something else —
change nothing and reply in a sentence or two. A light, dry touch suits it if it comes naturally; then
steer back to what the plan still needs before it can deploy, naming it.`

const repairPrompt = `You are repairing a deployment plan for Pando, a self-hosted platform that builds
and runs applications from their source repositories.

Pando analyzed this repository with deterministic detectors, and the result did not work: either
Pando started the application and it exited with an error, or none of the detectors could work out
how to build it. You are only called when that happens. Your job is not to produce a plan of your
own. It is to read the repository, and the log if there is one, and decide whether the failure can
be fixed with the changes below — or whether it is real, in which case you change nothing and a person
is shown the log.

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
- A question Pando could not answer that the repository in fact answers.`

const answerPrompt = `You are answering questions for Pando, a self-hosted platform that builds and runs
applications from their source repositories.

Pando analyzed this repository with deterministic detectors and produced a plan, but could not settle
everything: it has questions that would otherwise go to a person. You are only called when that
happens. Your job is to answer each question the repository actually answers, so that nobody has to,
and to leave the rest for a person. You may only answer questions — the plan is otherwise working, and
any other change you propose is discarded.`

const rulesPrompt = `Rules that are enforced, not advisory. An amendment breaking one of these is discarded:

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

An answer to a question must be one of its valid answers when it lists them, and must be something a
person could type into the question's field as-is: a port number, a path, a command. Not a
description of one.`

const repairClosing = `Read what you need, then call submit_findings exactly once. Submitting no amendments
is the right outcome when the failure is real — an application that needs something the repository
never mentions — or when you cannot tell what went wrong. Do not guess.`

const reviseClosing = `Read what you need, then call submit_findings exactly once, with the amendments the repository
supports and a reply to the person. The reply is one to three plain sentences: what you changed and the
file that shows it, or why you changed nothing. Submitting no amendments is right when the repository
does not support the request. Do not guess, and do not claim a change you did not submit.`

const answerClosing = `Read what you need, then call submit_findings exactly once, with an answer_question
amendment for each question the repository answers: "key" is the question's key exactly as listed,
"value" is the answer, and "evidence" names the files you read that settle it. Submitting none is the right outcome when the
repository does not settle any of them — a person will answer instead. Do not guess.`

// userPrompt is the proposal, rendered.
func userPrompt(fn api.AIFunction, req api.ScreenRequest) string {
	var b strings.Builder

	switch fn {
	case api.AIFunctionAnswerQuestions:
		b.WriteString("Here is the plan Pando produced for this repository, and the questions it could not answer.\n\n")
	case api.AIFunctionRevisePlan:
		b.WriteString("Here is the plan Pando produced for this repository, which a person is reviewing.\n\n")
	default:
		b.WriteString("Here is the plan Pando produced for this repository, which did not work.\n\n")
	}
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

	if len(req.Values) > 0 {
		b.WriteString("\n## Values the deploy waits on\n\n")
		b.WriteString("Nobody has set these, and the app cannot deploy without them. Fill one with a " +
			"`set_env` amendment only when the repository settles its value — or, for the app's own URL " +
			"or domain, the routing in the plan above does. Never make up a secret, key, token or " +
			"password: leave those for the person, who can generate one.\n\n")
		for _, key := range req.Values {
			fmt.Fprintf(&b, "- %s\n", key)
		}
	}

	if fn == api.AIFunctionRevisePlan {
		if len(req.Conversation) > 0 {
			b.WriteString("\n## What was said before\n\n")
			for _, t := range req.Conversation {
				who := "The person"
				if t.From == "ai" {
					who = "You"
				}
				fmt.Fprintf(&b, "%s: %s\n\n", who, t.Text)
			}
		}
		// Last, and quoted as theirs: it is the request, and it is a person's
		// words rather than instructions from Pando.
		b.WriteString("\n## What the person reviewing the plan says now\n\n")
		b.WriteString("> " + strings.ReplaceAll(strings.TrimSpace(req.Instruction), "\n", "\n> ") + "\n")
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
		// Where the app will be served, as far as the plan says: a hostname
		// in subdomain mode, a port on Pando's own host in port mode. What a
		// variable naming the app's own domain or URL should hold.
		Hostname string `json:"hostname,omitempty"`
		Port     int    `json:"port,omitempty"`
		Subdir   string `json:"subdir,omitempty"`
	}{
		Build:       s.Build,
		Volumes:     s.Volumes,
		Slots:       s.Slots,
		Health:      s.Health,
		Warnings:    s.Warnings,
		RoutingMode: s.Routing.Mode,
		Hostname:    s.Routing.Hostname,
		Port:        s.Routing.Port,
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
