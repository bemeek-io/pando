package idp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// The parts of SCIM 2.0 (RFC 7643, RFC 7644) that are parsing: filters and
// PATCH. Kept free of storage so they can be tested against the requests Okta
// and Entra actually send, which differ from each other and from the RFC's
// examples — Entra sends "False" as a string, capitalizes ops, and addresses
// emails by filter; Okta replaces with a value object and no path.

// SCIM schema URNs.
const (
	SchemaUser         = "urn:ietf:params:scim:schemas:core:2.0:User"
	SchemaGroup        = "urn:ietf:params:scim:schemas:core:2.0:Group"
	SchemaEnterprise   = "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User"
	SchemaListResponse = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	SchemaPatchOp      = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	SchemaError        = "urn:ietf:params:scim:api:messages:2.0:Error"
)

// SCIMError is a SCIM error response (RFC 7644 §3.12).
type SCIMError struct {
	Status int
	Type   string
	Detail string
}

func (e *SCIMError) Error() string { return e.Detail }

// Body is the error as the client receives it.
func (e *SCIMError) Body() map[string]any {
	b := map[string]any{"schemas": []string{SchemaError}, "status": fmt.Sprint(e.Status), "detail": e.Detail}
	if e.Type != "" {
		b["scimType"] = e.Type
	}
	return b
}

func scimBad(scimType, format string, args ...any) *SCIMError {
	return &SCIMError{Status: http.StatusBadRequest, Type: scimType, Detail: fmt.Sprintf(format, args...)}
}

// Filter is the one form of SCIM filter Pando answers: an attribute equal to
// a value. It is what every provisioning client sends to ask "does this
// person already exist", and anything richer would be a query language kept
// for nobody.
type Filter struct {
	Attr  string
	Value string
}

var filterPattern = regexp.MustCompile(`(?i)^\s*([A-Za-z][A-Za-z0-9._:\-]*(?:\[[^\]]*\])?(?:\.[A-Za-z]+)?)\s+eq\s+"((?:[^"\\]|\\.)*)"\s*$`)

// ParseFilter reads `attr eq "value"`, with attr one of those allowed,
// matched without regard to case.
func ParseFilter(raw string, allowed ...string) (Filter, error) {
	if strings.TrimSpace(raw) == "" {
		return Filter{}, nil
	}
	m := filterPattern.FindStringSubmatch(raw)
	if m == nil {
		return Filter{}, scimBad("invalidFilter", "Pando answers filters of the form attribute eq \"value\", and %q is not one.", raw)
	}
	attr := m[1]
	if i := strings.Index(attr, "["); i >= 0 {
		// emails[type eq "work"].value and the like: the address, whatever
		// its type.
		attr = attr[:i] + attr[strings.Index(attr, "]")+1:]
	}
	attr = strings.TrimPrefix(attr, SchemaUser+":")
	attr = strings.TrimPrefix(attr, SchemaGroup+":")
	var value string
	if err := json.Unmarshal([]byte(`"`+m[2]+`"`), &value); err != nil {
		return Filter{}, scimBad("invalidFilter", "The filter's value could not be read.")
	}
	for _, a := range allowed {
		if strings.EqualFold(a, attr) {
			return Filter{Attr: a, Value: value}, nil
		}
	}
	return Filter{}, scimBad("invalidFilter", "Pando cannot filter by %q. It can filter by %s.", attr, strings.Join(allowed, ", "))
}

// PatchOp is one operation of a PATCH request.
type PatchOp struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

// PatchRequest is a SCIM PATCH body.
type PatchRequest struct {
	Schemas    []string  `json:"schemas"`
	Operations []PatchOp `json:"Operations"`
}

// ApplyPatch applies operations to a resource held as a map. The caller
// validates the result as it would a PUT, so a patch can do nothing a
// replacement could not.
//
// Members of a group are not handled here: they are relations, not
// attributes, and the caller takes them out first (MemberOps).
func ApplyPatch(resource map[string]any, ops []PatchOp) error {
	for _, op := range ops {
		kind := strings.ToLower(op.Op)
		var value any
		if len(op.Value) > 0 {
			if err := json.Unmarshal(op.Value, &value); err != nil {
				return scimBad("invalidValue", "An operation's value could not be read.")
			}
		}
		switch kind {
		case "add", "replace":
			if op.Path == "" {
				obj, ok := value.(map[string]any)
				if !ok {
					return scimBad("invalidValue", "An operation with no path needs an object as its value.")
				}
				for k, v := range obj {
					if err := setPath(resource, k, v); err != nil {
						return err
					}
				}
				continue
			}
			if err := setPath(resource, op.Path, value); err != nil {
				return err
			}
		case "remove":
			if op.Path == "" {
				return scimBad("noTarget", "A remove operation needs a path.")
			}
			if err := removePath(resource, op.Path); err != nil {
				return err
			}
		default:
			return scimBad("invalidSyntax", "%q is not a PATCH operation. Use add, replace or remove.", op.Op)
		}
	}
	return nil
}

var valuePathPattern = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]*)\[\s*([A-Za-z][A-Za-z0-9]*)\s+eq\s+"([^"]*)"\s*\](?:\.([A-Za-z][A-Za-z0-9]*))?$`)

// splitSchema turns "urn:…:enterprise:2.0:User:employeeNumber" into the
// extension's key and the attribute, and strips the core schema's own URN.
func splitSchema(path string) (container, attr string) {
	for _, core := range []string{SchemaUser + ":", SchemaGroup + ":"} {
		if len(path) > len(core) && strings.EqualFold(path[:len(core)], core) {
			return "", path[len(core):]
		}
	}
	if strings.HasPrefix(strings.ToLower(path), "urn:") {
		if i := strings.LastIndex(path, ":"); i > 0 {
			return path[:i], path[i+1:]
		}
	}
	return "", path
}

// key finds a map key without regard to case, as SCIM attribute names are
// case-insensitive, and returns the name to write under.
func key(m map[string]any, name string) string {
	for k := range m {
		if strings.EqualFold(k, name) {
			return k
		}
	}
	return name
}

func setPath(resource map[string]any, path string, value any) error {
	container, attr := splitSchema(path)
	target := resource
	if container != "" {
		k := key(resource, container)
		sub, _ := resource[k].(map[string]any)
		if sub == nil {
			sub = map[string]any{}
			resource[k] = sub
		}
		target = sub
	}
	if m := valuePathPattern.FindStringSubmatch(attr); m != nil {
		return setFiltered(target, m[1], m[2], m[3], m[4], value)
	}
	parts := strings.Split(attr, ".")
	for _, part := range parts[:len(parts)-1] {
		k := key(target, part)
		sub, _ := target[k].(map[string]any)
		if sub == nil {
			sub = map[string]any{}
			target[k] = sub
		}
		target = sub
	}
	target[key(target, parts[len(parts)-1])] = value
	return nil
}

// setFiltered sets emails[type eq "work"].value and similar: the matching
// element's sub-attribute, adding the element when none matches.
func setFiltered(target map[string]any, attr, fAttr, fValue, sub string, value any) error {
	k := key(target, attr)
	list, _ := target[k].([]any)
	for _, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if fmt.Sprint(obj[key(obj, fAttr)]) == fValue {
			if sub == "" {
				if v, ok := value.(map[string]any); ok {
					for kk, vv := range v {
						obj[key(obj, kk)] = vv
					}
				}
				return nil
			}
			obj[key(obj, sub)] = value
			return nil
		}
	}
	obj := map[string]any{fAttr: fValue}
	if sub == "" {
		if v, ok := value.(map[string]any); ok {
			for kk, vv := range v {
				obj[kk] = vv
			}
		}
	} else {
		obj[sub] = value
	}
	target[k] = append(list, obj)
	return nil
}

func removePath(resource map[string]any, path string) error {
	container, attr := splitSchema(path)
	target := resource
	if container != "" {
		sub, _ := resource[key(resource, container)].(map[string]any)
		if sub == nil {
			return nil
		}
		target = sub
	}
	if m := valuePathPattern.FindStringSubmatch(attr); m != nil {
		k := key(target, m[1])
		list, _ := target[k].([]any)
		kept := list[:0]
		for _, item := range list {
			obj, ok := item.(map[string]any)
			if ok && fmt.Sprint(obj[key(obj, m[2])]) == m[3] {
				if m[4] != "" {
					delete(obj, key(obj, m[4]))
					kept = append(kept, obj)
				}
				continue
			}
			kept = append(kept, item)
		}
		target[k] = kept
		return nil
	}
	parts := strings.Split(attr, ".")
	for _, part := range parts[:len(parts)-1] {
		sub, _ := target[key(target, part)].(map[string]any)
		if sub == nil {
			return nil
		}
		target = sub
	}
	delete(target, key(target, parts[len(parts)-1]))
	return nil
}

// MemberOps separates a group PATCH's membership operations from the rest.
// Returns who to add, who to remove, whether to clear the membership first
// (a replace, or a remove of "members" with no value), and the other ops.
func MemberOps(ops []PatchOp) (add, remove []string, clear bool, rest []PatchOp, err error) {
	memberFilter := regexp.MustCompile(`(?i)^members\[\s*value\s+eq\s+"([^"]+)"\s*\]$`)
	for _, op := range ops {
		kind := strings.ToLower(op.Op)
		path := op.Path
		_, attr := splitSchema(path)
		switch {
		case strings.EqualFold(attr, "members"):
			ids, perr := memberValues(op.Value)
			if perr != nil {
				return nil, nil, false, nil, perr
			}
			switch kind {
			case "add":
				add = append(add, ids...)
			case "replace":
				clear, add, remove = true, ids, nil
			case "remove":
				if len(ids) == 0 {
					clear, add, remove = true, nil, nil
				} else {
					remove = append(remove, ids...)
				}
			default:
				return nil, nil, false, nil, scimBad("invalidSyntax", "%q is not a PATCH operation.", op.Op)
			}
		case memberFilter.MatchString(attr):
			if kind != "remove" {
				return nil, nil, false, nil, scimBad("invalidPath", "Only remove can address one member by filter.")
			}
			remove = append(remove, memberFilter.FindStringSubmatch(attr)[1])
		case path == "" && (kind == "add" || kind == "replace"):
			// Okta's shape: {"op":"replace","value":{"id":…,"displayName":…,"members":[…]}}.
			var obj map[string]json.RawMessage
			if json.Unmarshal(op.Value, &obj) == nil {
				for k, v := range obj {
					if strings.EqualFold(k, "members") {
						ids, perr := memberValues(v)
						if perr != nil {
							return nil, nil, false, nil, perr
						}
						if kind == "replace" {
							clear, add, remove = true, ids, nil
						} else {
							add = append(add, ids...)
						}
						delete(obj, k)
					}
				}
				body, _ := json.Marshal(obj)
				rest = append(rest, PatchOp{Op: op.Op, Value: body})
				continue
			}
			rest = append(rest, op)
		default:
			rest = append(rest, op)
		}
	}
	return add, remove, clear, rest, nil
}

func memberValues(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var list []struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		var one struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal(raw, &one); err != nil {
			return nil, scimBad("invalidValue", "members must be a list of {\"value\": id}.")
		}
		list = append(list, one)
	}
	out := make([]string, 0, len(list))
	for _, m := range list {
		if m.Value != "" {
			out = append(out, m.Value)
		}
	}
	return out, nil
}

// boolValue reads a SCIM boolean, which some clients send as a string.
func boolValue(v any, def bool) (bool, error) {
	switch b := v.(type) {
	case nil:
		return def, nil
	case bool:
		return b, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(b)) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	return false, scimBad("invalidValue", "active must be true or false.")
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// lookup reads an attribute without regard to case.
func lookup(m map[string]any, name string) any {
	return m[key(m, name)]
}
