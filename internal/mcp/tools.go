package mcp

import (
	"encoding/base64"
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

// Bytes is a request body sent as-is rather than encoded as JSON, for the
// endpoints whose body is a file.
type Bytes struct {
	ContentType string
	Data        []byte
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
		Name: "pando_stop_app",
		Description: "Stop an app without deleting it. Its storage, configuration and address " +
			"are kept, and it stays stopped until something starts it again.",
		Schema: schema(map[string]any{"app_id": str("The app's ID.")}, "app_id"),
		request: func(args map[string]any) (string, string, any, error) {
			id, err := stringArg(args, "app_id", true)
			if err != nil {
				return "", "", nil, err
			}
			return "POST", appPath(id, "/stop"), map[string]any{}, nil
		},
	},
	{
		Name:        "pando_start_app",
		Description: "Start an app that was stopped, bringing back the version that was running.",
		Schema:      schema(map[string]any{"app_id": str("The app's ID.")}, "app_id"),
		request: func(args map[string]any) (string, string, any, error) {
			id, err := stringArg(args, "app_id", true)
			if err != nil {
				return "", "", nil, err
			}
			return "POST", appPath(id, "/start"), map[string]any{}, nil
		},
	},
	{
		Name: "pando_restart_app",
		Description: "Restart an app's workloads in place. Nothing is rebuilt and nothing is " +
			"re-read — the same version, started again.",
		Schema: schema(map[string]any{"app_id": str("The app's ID.")}, "app_id"),
		request: func(args map[string]any) (string, string, any, error) {
			id, err := stringArg(args, "app_id", true)
			if err != nil {
				return "", "", nil, err
			}
			return "POST", appPath(id, "/restart"), map[string]any{}, nil
		},
	},
	{
		Name: "pando_set_app_icon",
		Description: "Set the image shown on an app's launcher tile. The image is a PNG, JPEG, " +
			"WebP or GIF file of at most 256 KB, base64-encoded. SVG is not accepted.",
		Schema: schema(map[string]any{
			"app_id":       str("The app's ID."),
			"image_base64": str("The image file's bytes, base64-encoded."),
		}, "app_id", "image_base64"),
		request: func(args map[string]any) (string, string, any, error) {
			id, err := stringArg(args, "app_id", true)
			if err != nil {
				return "", "", nil, err
			}
			encoded, err := stringArg(args, "image_base64", true)
			if err != nil {
				return "", "", nil, err
			}
			data, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return "", "", nil, fmt.Errorf("image_base64 is not valid base64: %w", err)
			}
			return "PUT", appPath(id, "/icon"), Bytes{ContentType: "application/octet-stream", Data: data}, nil
		},
	},
	{
		Name:        "pando_clear_app_icon",
		Description: "Remove the image on an app's launcher tile, so the tile shows the map generated for it.",
		Schema:      schema(map[string]any{"app_id": str("The app's ID.")}, "app_id"),
		request: func(args map[string]any) (string, string, any, error) {
			id, err := stringArg(args, "app_id", true)
			if err != nil {
				return "", "", nil, err
			}
			return "DELETE", appPath(id, "/icon"), nil, nil
		},
	},
	{
		Name: "pando_favorite_app",
		Description: "Pin an app to the top of your own launcher. It grants nothing and only you " +
			"see it; you must be able to open the app.",
		Schema: schema(map[string]any{"app_id": str("The app's ID.")}, "app_id"),
		request: func(args map[string]any) (string, string, any, error) {
			id, err := stringArg(args, "app_id", true)
			if err != nil {
				return "", "", nil, err
			}
			return "PUT", "/me/favorites/" + url.PathEscape(id), nil, nil
		},
	},
	{
		Name:        "pando_unfavorite_app",
		Description: "Unpin an app from your launcher.",
		Schema:      schema(map[string]any{"app_id": str("The app's ID.")}, "app_id"),
		request: func(args map[string]any) (string, string, any, error) {
			id, err := stringArg(args, "app_id", true)
			if err != nil {
				return "", "", nil, err
			}
			return "DELETE", "/me/favorites/" + url.PathEscape(id), nil, nil
		},
	},
	{
		Name:        "pando_rename_app",
		Description: "Change an app's display name. Its ID and address do not change.",
		Schema: schema(map[string]any{
			"app_id": str("The app's ID."),
			"name":   str("The new name."),
		}, "app_id", "name"),
		request: func(args map[string]any) (string, string, any, error) {
			id, err := stringArg(args, "app_id", true)
			if err != nil {
				return "", "", nil, err
			}
			name, err := stringArg(args, "name", true)
			if err != nil {
				return "", "", nil, err
			}
			return "PATCH", appPath(id, ""), map[string]any{"name": name}, nil
		},
	},
	{
		Name: "pando_list_my_apps",
		Description: "The apps you can open — your launcher — with which are favorites and which of " +
			"your sections each is filed under, and your sections. A different list from " +
			"pando_list_apps, which is the apps you can administer.",
		Schema: schema(map[string]any{}),
		request: func(map[string]any) (string, string, any, error) {
			return "GET", "/me/apps", nil, nil
		},
	},
	{
		Name:        "pando_create_section",
		Description: "Make a section in your own launcher: a named grouping of apps. Only you see it.",
		Schema:      schema(map[string]any{"name": str("The section's name.")}, "name"),
		request: func(args map[string]any) (string, string, any, error) {
			name, err := stringArg(args, "name", true)
			if err != nil {
				return "", "", nil, err
			}
			return "POST", "/me/sections", map[string]any{"name": name}, nil
		},
	},
	{
		Name:        "pando_list_user_apps",
		Description: "The apps an account has access to: its role for managing each, directly or through a group, whether it can use each, and whether you can change that (can_manage).",
		Schema: schema(map[string]any{
			"user_id": str("The account's ID."),
		}, "user_id"),
		request: func(args map[string]any) (string, string, any, error) {
			id, err := stringArg(args, "user_id", true)
			if err != nil {
				return "", "", nil, err
			}
			return "GET", "/users/" + url.PathEscape(id) + "/apps", nil, nil
		},
	},
	{
		Name:        "pando_rename_section",
		Description: "Rename one of your launcher sections.",
		Schema: schema(map[string]any{
			"section_id": str("The section's ID."),
			"name":       str("The new name."),
		}, "section_id", "name"),
		request: func(args map[string]any) (string, string, any, error) {
			id, err := stringArg(args, "section_id", true)
			if err != nil {
				return "", "", nil, err
			}
			name, err := stringArg(args, "name", true)
			if err != nil {
				return "", "", nil, err
			}
			return "PATCH", "/me/sections/" + url.PathEscape(id), map[string]any{"name": name}, nil
		},
	},
	{
		Name:        "pando_delete_section",
		Description: "Delete one of your launcher sections. Its apps go back to Your apps; nothing else changes.",
		Schema:      schema(map[string]any{"section_id": str("The section's ID.")}, "section_id"),
		request: func(args map[string]any) (string, string, any, error) {
			id, err := stringArg(args, "section_id", true)
			if err != nil {
				return "", "", nil, err
			}
			return "DELETE", "/me/sections/" + url.PathEscape(id), nil, nil
		},
	},
	{
		Name:        "pando_add_app_to_section",
		Description: "Move an app you can open into one of your launcher sections, out of any other.",
		Schema: schema(map[string]any{
			"section_id": str("The section's ID."),
			"app_id":     str("The app's ID."),
		}, "section_id", "app_id"),
		request: func(args map[string]any) (string, string, any, error) {
			sid, err := stringArg(args, "section_id", true)
			if err != nil {
				return "", "", nil, err
			}
			aid, err := stringArg(args, "app_id", true)
			if err != nil {
				return "", "", nil, err
			}
			return "PUT", "/me/sections/" + url.PathEscape(sid) + "/apps/" + url.PathEscape(aid), nil, nil
		},
	},
	{
		Name:        "pando_remove_app_from_section",
		Description: "Move an app out of one of your launcher sections, back to Your apps.",
		Schema: schema(map[string]any{
			"section_id": str("The section's ID."),
			"app_id":     str("The app's ID."),
		}, "section_id", "app_id"),
		request: func(args map[string]any) (string, string, any, error) {
			sid, err := stringArg(args, "section_id", true)
			if err != nil {
				return "", "", nil, err
			}
			aid, err := stringArg(args, "app_id", true)
			if err != nil {
				return "", "", nil, err
			}
			return "DELETE", "/me/sections/" + url.PathEscape(sid) + "/apps/" + url.PathEscape(aid), nil, nil
		},
	},
	{
		Name: "pando_list_audit",
		Description: "Read the audit log, newest first. Every filter is optional and they combine: " +
			"what was done (an action prefix such as app. or grant.delete), who did it, which app, " +
			"what it was done to, and when (RFC 3339 times; since inclusive, until exclusive).",
		Schema: schema(map[string]any{
			"action":         str("Actions starting with this, e.g. app."),
			"principal_id":   str("Who did it: a user or token ID, or system, reconciler or detection."),
			"principal_kind": str("What kind of actor: user, token, system or anonymous."),
			"app_id":         str("Events on this app."),
			"target_kind":    str("What kind of thing it was done to, e.g. user, role, app."),
			"target_id":      str("The ID of the thing it was done to."),
			"involving":      str("Events where this ID is the actor or the target: everything to do with one account."),
			"since":          str("From this time, RFC 3339."),
			"until":          str("Up to this time, RFC 3339."),
			"before":         str("The next_before from a previous page, to read further back."),
		}),
		request: func(args map[string]any) (string, string, any, error) {
			q := url.Values{}
			for _, key := range []string{"action", "principal_id", "principal_kind", "app_id", "target_kind", "target_id", "involving", "since", "until", "before"} {
				v, err := stringArg(args, key, false)
				if err != nil {
					return "", "", nil, err
				}
				if v != "" {
					q.Set(key, v)
				}
			}
			path := "/audit"
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			return "GET", path, nil, nil
		},
	},
	{
		Name: "pando_get_config",
		Description: "The configuration the Pando server started with: every non-secret setting, " +
			"its value and where it was set (an environment variable, the config file, or the " +
			"default), and the host policy fields fixed there, which cannot be changed through " +
			"the API while they are set.",
		Schema: schema(map[string]any{}),
		request: func(map[string]any) (string, string, any, error) {
			return "GET", "/config", nil, nil
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
	{
		Name: "pando_get_usage",
		Description: "What each part of an app is using right now: CPU (thousandths of a core), " +
			"memory and disk in bytes, and each mounted volume's size, beside its limits " +
			"(0 means none). A reading, not a history.",
		Schema: schema(map[string]any{"app_id": str("The app's ID.")}, "app_id"),
		request: func(args map[string]any) (string, string, any, error) {
			id, err := stringArg(args, "app_id", true)
			if err != nil {
				return "", "", nil, err
			}
			return "GET", appPath(id, "/usage"), nil, nil
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
