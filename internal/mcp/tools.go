package mcp

import (
	"fmt"
	"net/url"
)

// The tool catalog (design 04 §3).
//
// One tool per endpoint, and the mapping is deliberately boring: a tool that
// composed several calls would be a capability the CLI and console do not have,
// which is the thing R-261 forbids. If an agent needs a workflow, it makes the
// calls.
//
// Exec, secret value reads, grant mutation, policy mutation and user deletion
// are absent. Not because this list is the boundary — host policy is (O-12) —
// but because offering a tool the policy will refuse wastes the agent's turn
// and teaches it that Pando's tools fail randomly.

type tool struct {
	Name        string
	Description string
	Schema      map[string]any

	// request turns arguments into an API call.
	request func(args map[string]any) (method, path string, body any, err error)
}

func stringArg(args map[string]any, key string, required bool) (string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		if required {
			return "", fmt.Errorf("%s is required", key)
		}
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	if required && s == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return s, nil
}

// appPath builds a path under an app, escaping the ID.
//
// Escaped even though app IDs are Pando's own prefixed ULIDs: the ID here comes
// from an agent, which means it comes from a model, which means it can be
// anything at all.
func appPath(id, suffix string) string {
	return "/apps/" + url.PathEscape(id) + suffix
}

func schema(props map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"type":       "object",
		"properties": props,
		"required":   required,
	}
}

func str(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

var toolList = []tool{
	{
		Name:        "pando_list_apps",
		Description: "List the apps you can manage, with their current state.",
		Schema:      schema(map[string]any{}),
		request: func(map[string]any) (string, string, any, error) {
			return "GET", "/apps", nil, nil
		},
	},
	{
		Name:        "pando_get_app",
		Description: "Get one app: its name, source, state and pinned spec.",
		Schema:      schema(map[string]any{"app_id": str("The app's ID.")}, "app_id"),
		request: func(args map[string]any) (string, string, any, error) {
			id, err := stringArg(args, "app_id", true)
			if err != nil {
				return "", "", nil, err
			}
			return "GET", appPath(id, ""), nil, nil
		},
	},
	{
		Name: "pando_create_app",
		Description: "Create an app from a git repository. Returns immediately with the app in " +
			"draft while Pando works out how to run it; call pando_get_detection next.",
		Schema: schema(map[string]any{
			"name":       str("A short name for the app."),
			"source_url": str("The repository URL."),
			"ref":        str("Branch or tag. Optional."),
		}, "name", "source_url"),
		request: func(args map[string]any) (string, string, any, error) {
			name, err := stringArg(args, "name", true)
			if err != nil {
				return "", "", nil, err
			}
			source, err := stringArg(args, "source_url", true)
			if err != nil {
				return "", "", nil, err
			}
			ref, _ := stringArg(args, "ref", false)
			return "POST", "/apps", map[string]any{
				"name":   name,
				"source": map[string]string{"type": "git", "url": source, "ref": ref},
			}, nil
		},
	},
	{
		Name: "pando_get_detection",
		Description: "What Pando worked out about an app, including any questions it needs " +
			"answered before it can deploy. The questions are written to be answerable by " +
			"whatever wrote the app.",
		Schema: schema(map[string]any{"app_id": str("The app's ID.")}, "app_id"),
		request: func(args map[string]any) (string, string, any, error) {
			id, err := stringArg(args, "app_id", true)
			if err != nil {
				return "", "", nil, err
			}
			return "GET", appPath(id, "/detection"), nil, nil
		},
	},
	{
		Name:        "pando_answer_detection",
		Description: "Answer one of the questions from pando_get_detection.",
		Schema: schema(map[string]any{
			"app_id": str("The app's ID."),
			"key":    str("The question's key."),
			"answer": str("The answer."),
		}, "app_id", "key", "answer"),
		request: func(args map[string]any) (string, string, any, error) {
			id, err := stringArg(args, "app_id", true)
			if err != nil {
				return "", "", nil, err
			}
			key, err := stringArg(args, "key", true)
			if err != nil {
				return "", "", nil, err
			}
			answer, err := stringArg(args, "answer", true)
			if err != nil {
				return "", "", nil, err
			}
			return "POST", appPath(id, "/detection/answers"),
				map[string]any{"answers": map[string]string{key: answer}}, nil
		},
	},
	{
		Name: "pando_accept_proposal",
		Description: "Accept what Pando worked out and pin it as the app's setup. " +
			"This does not deploy — call pando_deploy after.",
		Schema: schema(map[string]any{"app_id": str("The app's ID.")}, "app_id"),
		request: func(args map[string]any) (string, string, any, error) {
			id, err := stringArg(args, "app_id", true)
			if err != nil {
				return "", "", nil, err
			}
			return "POST", appPath(id, "/detection/accept"), map[string]any{}, nil
		},
	},
	{
		Name: "pando_plan",
		Description: "Show what a deploy would do, without doing it. Side-effect free, so it is " +
			"safe to call after any change to check the change is deployable.",
		Schema: schema(map[string]any{"app_id": str("The app's ID.")}, "app_id"),
		request: func(args map[string]any) (string, string, any, error) {
			id, err := stringArg(args, "app_id", true)
			if err != nil {
				return "", "", nil, err
			}
			return "POST", appPath(id, "/plan"), map[string]any{}, nil
		},
	},
	{
		Name:        "pando_deploy",
		Description: "Deploy an app. Returns once the deployment has been accepted, not once it is running.",
		Schema: schema(map[string]any{
			"app_id":          str("The app's ID."),
			"idempotency_key": str("A key you choose. Retrying with the same key will not deploy twice."),
		}, "app_id"),
		request: func(args map[string]any) (string, string, any, error) {
			id, err := stringArg(args, "app_id", true)
			if err != nil {
				return "", "", nil, err
			}
			key, _ := stringArg(args, "idempotency_key", false)
			body := map[string]any{}
			if key != "" {
				body["idempotency_key"] = key
			}
			return "POST", appPath(id, "/deployments"), body, nil
		},
	},
	{
		Name: "pando_get_logs",
		Description: "Read an app's recent logs. An app can be made of several parts — a web " +
			"service, a worker, a database it brought with it — and each has its own log. " +
			"Without `workload` this is the primary part, the one the app's address resolves " +
			"to; pando_get_status lists the names.",
		Schema: schema(map[string]any{
			"app_id":   str("The app's ID."),
			"workload": str("Which part of the app to read. Defaults to the primary one."),
		}, "app_id"),
		request: func(args map[string]any) (string, string, any, error) {
			id, err := stringArg(args, "app_id", true)
			if err != nil {
				return "", "", nil, err
			}
			workload, err := stringArg(args, "workload", false)
			if err != nil {
				return "", "", nil, err
			}
			path := "/logs"
			if workload != "" {
				path += "?workload=" + url.QueryEscape(workload)
			}
			return "GET", appPath(id, path), nil, nil
		},
	},
	{
		Name: "pando_get_status",
		Description: "What an app is doing right now: running, degraded, failed, and why — " +
			"including each part separately, so a single part that is crash-looping is " +
			"visible rather than averaged into one word for the app.",
		Schema: schema(map[string]any{"app_id": str("The app's ID.")}, "app_id"),
		request: func(args map[string]any) (string, string, any, error) {
			id, err := stringArg(args, "app_id", true)
			if err != nil {
				return "", "", nil, err
			}
			return "GET", appPath(id, "/status"), nil, nil
		},
	},
}

var toolsByName = func() map[string]tool {
	m := make(map[string]tool, len(toolList))
	for _, t := range toolList {
		m[t.Name] = t
	}
	return m
}()
