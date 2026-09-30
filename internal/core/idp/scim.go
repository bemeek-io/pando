package idp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/trypando/pando/internal/core/audit"
	"github.com/trypando/pando/internal/core/state"
	"github.com/trypando/pando/internal/errs"
)

// SCIM 2.0 provisioning (R-048, RFC 7643/7644): a provider pushes who exists,
// who is active, and who is in which group, and Pando applies it at once.
//
// What a push may change is narrow on purpose. It creates accounts, suspends
// and reactivates them (never deletes — R-049), and sets membership of that
// provider's own groups. It cannot touch a role, a grant, a Pando-made group,
// or an account the provider has no identity on: the provider says who is in a
// group, and Pando says what the group can do (R-078).

// SCIMContext is who is pushing: the provider whose token was presented, and
// the address resources are reported at.
type SCIMContext struct {
	Provider state.IdentityProvider
	Base     string
}

// SCIMAuthenticate resolves a bearer token to its provider.
func (s *Service) SCIMAuthenticate(ctx context.Context, bearer string, o Origin) (SCIMContext, error) {
	if bearer == "" {
		return SCIMContext{}, &SCIMError{Status: http.StatusUnauthorized, Detail: "A SCIM request needs the provider's bearer token."}
	}
	p, found, err := s.Providers.BySCIMToken(ctx, Digest(bearer))
	if err != nil {
		return SCIMContext{}, err
	}
	if !found {
		return SCIMContext{}, &SCIMError{Status: http.StatusUnauthorized,
			Detail: "That SCIM token is not one Pando issued, or it was replaced. Generate a new one on the provider's page in Pando."}
	}
	if !p.Enabled {
		return SCIMContext{}, &SCIMError{Status: http.StatusForbidden,
			Detail: "This identity provider is turned off in Pando, so it cannot provision accounts."}
	}
	return SCIMContext{Provider: p, Base: s.SCIMBaseURL(o)}, nil
}

func (c SCIMContext) actor() (audit.PrincipalKind, string) {
	return audit.KindSystem, "scim:" + c.Provider.ID
}

func (s *Service) scimAudit(ctx context.Context, c SCIMContext, action, targetKind, targetID string, detail map[string]any) {
	kind, actor := c.actor()
	if detail == nil {
		detail = map[string]any{}
	}
	detail["adapter_id"] = c.Provider.ID
	detail["via"] = "scim"
	s.audit(ctx, audit.Event{PrincipalKind: kind, PrincipalID: actor, Action: action,
		TargetKind: targetKind, TargetID: targetID, Detail: detail})
}

// scimErr turns a store error into a SCIM one.
func scimErr(err error) error {
	var se *SCIMError
	if errors.As(err, &se) {
		return se
	}
	if errors.Is(err, state.ErrSCIMConflict) {
		return &SCIMError{Status: http.StatusConflict, Type: "uniqueness", Detail: err.Error()}
	}
	e := errs.As(err)
	if e == nil {
		return &SCIMError{Status: http.StatusInternalServerError, Detail: "Pando could not complete the request."}
	}
	switch e.Code {
	case errs.NotFound:
		return &SCIMError{Status: http.StatusNotFound, Detail: e.Message}
	case errs.ValidInvalid:
		if e.Message == state.ErrSCIMConflict.Message {
			return &SCIMError{Status: http.StatusConflict, Type: "uniqueness", Detail: e.Message}
		}
		d := e.Message
		if e.Remedy != "" {
			d += " " + e.Remedy
		}
		return &SCIMError{Status: http.StatusConflict, Type: "mutability", Detail: d}
	}
	return &SCIMError{Status: http.StatusInternalServerError, Detail: e.Message}
}

// Paging reads startIndex and count (1-based, RFC 7644 §3.4.2.4).
func Paging(startIndex, count int) (offset, limit int) {
	if startIndex < 1 {
		startIndex = 1
	}
	if count < 0 {
		count = 0
	}
	if count == 0 && startIndex == 1 {
		count = 100
	}
	if count > 200 {
		count = 200
	}
	return startIndex - 1, count
}

func listResponse(resources []map[string]any, total, offset int) map[string]any {
	if resources == nil {
		resources = []map[string]any{}
	}
	return map[string]any{
		"schemas": []string{SchemaListResponse}, "totalResults": total,
		"startIndex": offset + 1, "itemsPerPage": len(resources), "Resources": resources,
	}
}

func meta(kind, location string, created, updated time.Time) map[string]any {
	return map[string]any{"resourceType": kind, "location": location,
		"created": created.UTC().Format(time.RFC3339), "lastModified": updated.UTC().Format(time.RFC3339)}
}

// --- Users ------------------------------------------------------------------

func (s *Service) userResource(ctx context.Context, c SCIMContext, u state.SCIMUser) (map[string]any, error) {
	r := map[string]any{}
	_ = json.Unmarshal(u.Resource, &r)
	r["schemas"] = schemasFor(r, SchemaUser)
	r["id"] = u.ID
	r["userName"] = u.UserName
	if u.ExternalID != "" {
		r["externalId"] = u.ExternalID
	}
	r["active"] = u.Active()
	groups, err := s.SCIMGroups.UserGroups(ctx, c.Provider.ID, u.ID)
	if err != nil {
		return nil, err
	}
	gs := make([]map[string]any, 0, len(groups))
	for _, g := range groups {
		gs = append(gs, map[string]any{"value": g.UserID, "display": g.Display, "$ref": c.Base + "/Groups/" + g.UserID})
	}
	r["groups"] = gs
	r["meta"] = meta("User", c.Base+"/Users/"+u.ID, u.CreatedAt, u.UpdatedAt)
	return r, nil
}

// schemasFor keeps the extension schemas a resource uses.
func schemasFor(r map[string]any, core string) []string {
	out := []string{core}
	for k := range r {
		if strings.HasPrefix(strings.ToLower(k), "urn:") && !strings.EqualFold(k, core) {
			out = append(out, k)
		}
	}
	return out
}

// userFields is what Pando reads from a user resource.
type userFields struct {
	UserName, ExternalID, Email, DisplayName, Key string
	Active                                        bool
	Resource                                      []byte
}

func (s *Service) readUser(c SCIMContext, r map[string]any) (userFields, error) {
	for _, k := range []string{"id", "meta", "schemas", "password", "groups"} {
		delete(r, key(r, k))
	}
	f := userFields{UserName: stringValue(lookup(r, "userName")), ExternalID: stringValue(lookup(r, "externalId"))}
	if f.UserName == "" {
		return userFields{}, scimBad("invalidValue", "A user needs a userName.")
	}
	active, err := boolValue(lookup(r, "active"), true)
	if err != nil {
		return userFields{}, err
	}
	f.Active = active
	r[key(r, "active")] = active

	f.DisplayName = stringValue(lookup(r, "displayName"))
	if name, ok := lookup(r, "name").(map[string]any); ok {
		if f.DisplayName == "" {
			f.DisplayName = stringValue(lookup(name, "formatted"))
		}
		if f.DisplayName == "" {
			f.DisplayName = strings.TrimSpace(stringValue(lookup(name, "givenName")) + " " + stringValue(lookup(name, "familyName")))
		}
	}
	if emails, ok := lookup(r, "emails").([]any); ok {
		for _, e := range emails {
			obj, _ := e.(map[string]any)
			v := stringValue(lookup(obj, "value"))
			if v == "" {
				continue
			}
			primary, _ := boolValue(lookup(obj, "primary"), false)
			if f.Email == "" || primary {
				f.Email = v
			}
		}
	}

	switch c.Provider.SCIMIdentityAttribute {
	case "userName":
		f.Key = f.UserName
	default:
		f.Key = f.ExternalID
		if f.Key == "" {
			return userFields{}, scimBad("invalidValue",
				"%s matches SCIM users to sign-ins by externalId, and this user has none. Map externalId in the "+
					"provider to the value it signs people in with (the sub claim, or oid for Entra), or set this "+
					"provider's SCIM identity attribute to userName in Pando.", c.Provider.Name)
		}
	}
	body, err := json.Marshal(r)
	if err != nil {
		return userFields{}, scimBad("invalidValue", "The user could not be read.")
	}
	f.Resource = body
	return f, nil
}

// SCIMListUsers answers GET /Users.
func (s *Service) SCIMListUsers(ctx context.Context, c SCIMContext, filter string, startIndex, count int) (map[string]any, error) {
	f, err := ParseFilter(filter, "userName", "externalId", "id", "emails.value", "emails")
	if err != nil {
		return nil, err
	}
	offset, limit := Paging(startIndex, count)
	users, total, err := s.SCIMUsers.List(ctx, c.Provider.ID, f.Attr, f.Value, offset, limit)
	if err != nil {
		return nil, scimErr(err)
	}
	out := make([]map[string]any, 0, len(users))
	for _, u := range users {
		r, err := s.userResource(ctx, c, u)
		if err != nil {
			return nil, scimErr(err)
		}
		out = append(out, r)
	}
	return listResponse(out, total, offset), nil
}

// SCIMGetUser answers GET /Users/{id}.
func (s *Service) SCIMGetUser(ctx context.Context, c SCIMContext, userID string) (map[string]any, error) {
	u, found, err := s.SCIMUsers.ByID(ctx, c.Provider.ID, userID)
	if err != nil {
		return nil, scimErr(err)
	}
	if !found {
		return nil, &SCIMError{Status: http.StatusNotFound, Detail: "There is no such user for this provider."}
	}
	r, err := s.userResource(ctx, c, u)
	if err != nil {
		return nil, scimErr(err)
	}
	return r, nil
}

// SCIMCreateUser answers POST /Users.
//
// An account the provider's sign-in already made (just-in-time) is adopted
// rather than refused: the client is telling Pando about someone it already
// knows, and a conflict would leave that person unmanaged.
func (s *Service) SCIMCreateUser(ctx context.Context, c SCIMContext, body []byte) (map[string]any, error) {
	r := map[string]any{}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, scimBad("invalidSyntax", "The request body is not a JSON object.")
	}
	f, err := s.readUser(c, r)
	if err != nil {
		return nil, err
	}
	existing, found, err := s.SCIMUsers.ByKey(ctx, c.Provider.ID, f.Key)
	if err != nil {
		return nil, scimErr(err)
	}
	var userID string
	if found {
		if existing.UserName != "" {
			return nil, &SCIMError{Status: http.StatusConflict, Type: "uniqueness",
				Detail: "A user with that identity already exists for this provider: " + existing.ID}
		}
		userID = existing.ID
		if err := s.writeUser(ctx, c, userID, f); err != nil {
			return nil, err
		}
		s.scimAudit(ctx, c, "user.scim.adopt", "user", userID, map[string]any{"user_name": f.UserName})
	} else {
		created, err := s.Identities.CreateExternal(ctx, state.NewExternal{
			AdapterID: c.Provider.ID, ExternalID: f.Key, Email: f.Email, DisplayName: f.DisplayName,
			CreatedBy: "scim:" + c.Provider.ID, SCIMUserName: f.UserName, SCIMExternalID: f.ExternalID,
			SCIMResource: f.Resource, Suspended: !f.Active,
		})
		if err != nil {
			if e := errs.As(err); e != nil && e.Code == errs.ValidInvalid {
				return nil, &SCIMError{Status: http.StatusConflict, Type: "uniqueness", Detail: e.Message}
			}
			return nil, scimErr(err)
		}
		userID = created.ID
		s.scimAudit(ctx, c, "user.create", "user", userID, map[string]any{"user_name": f.UserName, "active": f.Active})
	}
	return s.SCIMGetUser(ctx, c, userID)
}

func (s *Service) writeUser(ctx context.Context, c SCIMContext, userID string, f userFields) error {
	if err := s.SCIMUsers.Write(ctx, c.Provider.ID, userID, state.SCIMUserUpdate{
		Key: f.Key, UserName: f.UserName, ExternalID: f.ExternalID, Resource: f.Resource,
		Email: f.Email, DisplayName: f.DisplayName,
	}); err != nil {
		return scimErr(err)
	}
	return s.setActive(ctx, c, userID, f.Active)
}

// setActive applies SCIM's active flag: false suspends and ends every session
// at once (R-048, R-049); true lifts only a suspension this provider made.
func (s *Service) setActive(ctx context.Context, c SCIMContext, userID string, active bool) error {
	u, found, err := s.Users.ByID(ctx, userID)
	if err != nil {
		return scimErr(err)
	}
	if !found {
		return &SCIMError{Status: http.StatusNotFound, Detail: "There is no such user for this provider."}
	}
	switch {
	case !active && u.Status == "active":
		if err := s.Users.SetStatusBy(ctx, userID, "suspended", state.SuspendedBySCIM(c.Provider.ID)); err != nil {
			return scimErr(err)
		}
		if err := s.Sessions.RevokeAllForUser(ctx, userID); err != nil {
			return scimErr(err)
		}
		s.scimAudit(ctx, c, "user.suspend", "user", userID, nil)
	case active && u.Status == "suspended":
		lifted, err := s.SCIMUsers.Reactivate(ctx, c.Provider.ID, userID)
		if err != nil {
			return scimErr(err)
		}
		if lifted {
			s.scimAudit(ctx, c, "user.activate", "user", userID, nil)
		}
	}
	return nil
}

// SCIMReplaceUser answers PUT /Users/{id}.
func (s *Service) SCIMReplaceUser(ctx context.Context, c SCIMContext, userID string, body []byte) (map[string]any, error) {
	if _, found, err := s.SCIMUsers.ByID(ctx, c.Provider.ID, userID); err != nil {
		return nil, scimErr(err)
	} else if !found {
		return nil, &SCIMError{Status: http.StatusNotFound, Detail: "There is no such user for this provider."}
	}
	r := map[string]any{}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, scimBad("invalidSyntax", "The request body is not a JSON object.")
	}
	f, err := s.readUser(c, r)
	if err != nil {
		return nil, err
	}
	if err := s.writeUser(ctx, c, userID, f); err != nil {
		return nil, err
	}
	s.scimAudit(ctx, c, "user.scim.update", "user", userID, map[string]any{"user_name": f.UserName, "active": f.Active})
	return s.SCIMGetUser(ctx, c, userID)
}

// SCIMPatchUser answers PATCH /Users/{id}.
func (s *Service) SCIMPatchUser(ctx context.Context, c SCIMContext, userID string, body []byte) (map[string]any, error) {
	u, found, err := s.SCIMUsers.ByID(ctx, c.Provider.ID, userID)
	if err != nil {
		return nil, scimErr(err)
	}
	if !found {
		return nil, &SCIMError{Status: http.StatusNotFound, Detail: "There is no such user for this provider."}
	}
	var req PatchRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, scimBad("invalidSyntax", "The request body is not a SCIM PatchOp.")
	}
	current, err := s.userResource(ctx, c, u)
	if err != nil {
		return nil, scimErr(err)
	}
	if err := ApplyPatch(current, req.Operations); err != nil {
		return nil, err
	}
	f, err := s.readUser(c, current)
	if err != nil {
		return nil, err
	}
	if err := s.writeUser(ctx, c, userID, f); err != nil {
		return nil, err
	}
	s.scimAudit(ctx, c, "user.scim.update", "user", userID, map[string]any{"user_name": f.UserName, "active": f.Active})
	return s.SCIMGetUser(ctx, c, userID)
}

// SCIMDeleteUser answers DELETE /Users/{id}: the account is suspended, not
// deleted (R-049), and the client forgets it.
func (s *Service) SCIMDeleteUser(ctx context.Context, c SCIMContext, userID string) error {
	if _, found, err := s.SCIMUsers.ByID(ctx, c.Provider.ID, userID); err != nil {
		return scimErr(err)
	} else if !found {
		return &SCIMError{Status: http.StatusNotFound, Detail: "There is no such user for this provider."}
	}
	if err := s.setActive(ctx, c, userID, false); err != nil {
		return err
	}
	if err := s.SCIMUsers.Forget(ctx, c.Provider.ID, userID); err != nil {
		return scimErr(err)
	}
	s.scimAudit(ctx, c, "user.scim.delete", "user", userID, nil)
	return nil
}

// --- Groups -------------------------------------------------------------------

func (s *Service) groupResource(c SCIMContext, g state.SCIMGroup, withMembers bool) map[string]any {
	r := map[string]any{}
	_ = json.Unmarshal(g.Resource, &r)
	r["schemas"] = []string{SchemaGroup}
	r["id"] = g.ID
	r["displayName"] = g.DisplayName
	if g.ExternalID != "" {
		r["externalId"] = g.ExternalID
	}
	if withMembers {
		ms := make([]map[string]any, 0, len(g.Members))
		for _, m := range g.Members {
			ms = append(ms, map[string]any{"value": m.UserID, "display": m.Display, "$ref": c.Base + "/Users/" + m.UserID})
		}
		r["members"] = ms
	} else {
		delete(r, "members")
	}
	created, _ := time.Parse(time.RFC3339, g.CreatedAt)
	updated, _ := time.Parse(time.RFC3339, g.UpdatedAt)
	r["meta"] = meta("Group", c.Base+"/Groups/"+g.ID, created, updated)
	return r
}

type groupFields struct {
	DisplayName, ExternalID string
	Members                 []string
	HasMembers              bool
	Resource                []byte
}

func readGroup(r map[string]any) (groupFields, error) {
	f := groupFields{DisplayName: stringValue(lookup(r, "displayName")), ExternalID: stringValue(lookup(r, "externalId"))}
	if f.DisplayName == "" {
		return groupFields{}, scimBad("invalidValue", "A group needs a displayName.")
	}
	if raw, ok := r[key(r, "members")]; ok {
		f.HasMembers = true
		body, _ := json.Marshal(raw)
		ids, err := memberValues(body)
		if err != nil {
			return groupFields{}, err
		}
		f.Members = ids
	}
	for _, k := range []string{"id", "meta", "schemas", "members"} {
		delete(r, key(r, k))
	}
	body, _ := json.Marshal(r)
	f.Resource = body
	return f, nil
}

// SCIMListGroups answers GET /Groups.
func (s *Service) SCIMListGroups(ctx context.Context, c SCIMContext, filter string, startIndex, count int, excludeMembers bool) (map[string]any, error) {
	f, err := ParseFilter(filter, "displayName", "externalId", "id")
	if err != nil {
		return nil, err
	}
	offset, limit := Paging(startIndex, count)
	groups, total, err := s.SCIMGroups.List(ctx, c.Provider.ID, f.Attr, f.Value, offset, limit, !excludeMembers)
	if err != nil {
		return nil, scimErr(err)
	}
	out := make([]map[string]any, 0, len(groups))
	for _, g := range groups {
		out = append(out, s.groupResource(c, g, !excludeMembers))
	}
	return listResponse(out, total, offset), nil
}

// SCIMGetGroup answers GET /Groups/{id}.
func (s *Service) SCIMGetGroup(ctx context.Context, c SCIMContext, groupID string, excludeMembers bool) (map[string]any, error) {
	g, found, err := s.SCIMGroups.ByID(ctx, c.Provider.ID, groupID, !excludeMembers)
	if err != nil {
		return nil, scimErr(err)
	}
	if !found {
		return nil, &SCIMError{Status: http.StatusNotFound, Detail: "There is no such group for this provider."}
	}
	return s.groupResource(c, g, !excludeMembers), nil
}

// SCIMCreateGroup answers POST /Groups. The group has no access until an
// administrator gives it some, or links it to a Pando group (R-078).
func (s *Service) SCIMCreateGroup(ctx context.Context, c SCIMContext, body []byte) (map[string]any, error) {
	r := map[string]any{}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, scimBad("invalidSyntax", "The request body is not a JSON object.")
	}
	f, err := readGroup(r)
	if err != nil {
		return nil, err
	}
	groupID, err := s.SCIMGroups.Create(ctx, c.Provider.ID, f.DisplayName, f.ExternalID, f.Resource, f.Members)
	if err != nil {
		return nil, scimErr(err)
	}
	s.scimAudit(ctx, c, "group.create", "group", groupID, map[string]any{"name": f.DisplayName, "members": len(f.Members)})
	return s.SCIMGetGroup(ctx, c, groupID, false)
}

// SCIMReplaceGroup answers PUT /Groups/{id}.
func (s *Service) SCIMReplaceGroup(ctx context.Context, c SCIMContext, groupID string, body []byte) (map[string]any, error) {
	r := map[string]any{}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, scimBad("invalidSyntax", "The request body is not a JSON object.")
	}
	f, err := readGroup(r)
	if err != nil {
		return nil, err
	}
	if err := s.SCIMGroups.Update(ctx, c.Provider.ID, groupID, &f.DisplayName, f.Resource, f.Members, true); err != nil {
		return nil, scimErr(err)
	}
	s.scimAudit(ctx, c, "group.members.set", "group", groupID, map[string]any{"name": f.DisplayName, "members": len(f.Members)})
	return s.SCIMGetGroup(ctx, c, groupID, false)
}

// SCIMPatchGroup answers PATCH /Groups/{id}: membership changes, a rename.
// Answers 204, as Entra and Okta both accept, rather than recomputing a
// membership list the client did not ask to see.
func (s *Service) SCIMPatchGroup(ctx context.Context, c SCIMContext, groupID string, body []byte) error {
	g, found, err := s.SCIMGroups.ByID(ctx, c.Provider.ID, groupID, false)
	if err != nil {
		return scimErr(err)
	}
	if !found {
		return &SCIMError{Status: http.StatusNotFound, Detail: "There is no such group for this provider."}
	}
	var req PatchRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return scimBad("invalidSyntax", "The request body is not a SCIM PatchOp.")
	}
	add, remove, clear, rest, err := MemberOps(req.Operations)
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		current := s.groupResource(c, g, false)
		if err := ApplyPatch(current, rest); err != nil {
			return err
		}
		f, err := readGroup(current)
		if err != nil {
			return err
		}
		if err := s.SCIMGroups.Update(ctx, c.Provider.ID, groupID, &f.DisplayName, f.Resource, nil, false); err != nil {
			return scimErr(err)
		}
		if f.DisplayName != g.DisplayName {
			s.scimAudit(ctx, c, "group.rename", "group", groupID, map[string]any{"name": f.DisplayName})
		}
	}
	if clear {
		if err := s.SCIMGroups.Update(ctx, c.Provider.ID, groupID, nil, nil, add, true); err != nil {
			return scimErr(err)
		}
		s.scimAudit(ctx, c, "group.members.set", "group", groupID, map[string]any{"members": len(add)})
		return nil
	}
	if len(add) > 0 || len(remove) > 0 {
		if err := s.SCIMGroups.ChangeMembers(ctx, c.Provider.ID, groupID, add, remove); err != nil {
			return scimErr(err)
		}
		s.scimAudit(ctx, c, "group.members.change", "group", groupID, map[string]any{"added": add, "removed": remove})
	}
	return nil
}

// SCIMDeleteGroup answers DELETE /Groups/{id}. The group goes, and every
// grant made to it with it — as when an administrator deletes one — unless
// that would leave nobody who can manage accounts (R-088).
func (s *Service) SCIMDeleteGroup(ctx context.Context, c SCIMContext, groupID string) error {
	if _, found, err := s.SCIMGroups.ByID(ctx, c.Provider.ID, groupID, false); err != nil {
		return scimErr(err)
	} else if !found {
		return &SCIMError{Status: http.StatusNotFound, Detail: "There is no such group for this provider."}
	}
	if err := s.Groups.Delete(ctx, groupID); err != nil {
		return scimErr(err)
	}
	s.scimAudit(ctx, c, "group.delete", "group", groupID, nil)
	return nil
}

// SCIMServiceProviderConfig answers GET /ServiceProviderConfig.
func (s *Service) SCIMServiceProviderConfig(c SCIMContext) map[string]any {
	return map[string]any{
		"schemas":          []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		"documentationUri": "https://github.com/trypando/pando/blob/main/docs/identity-providers.md",
		"patch":            map[string]any{"supported": true},
		"bulk":             map[string]any{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
		"filter":           map[string]any{"supported": true, "maxResults": 200},
		"changePassword":   map[string]any{"supported": false},
		"sort":             map[string]any{"supported": false},
		"etag":             map[string]any{"supported": false},
		"authenticationSchemes": []map[string]any{{
			"type": "oauthbearertoken", "name": "Bearer token", "primary": true,
			"description": "The SCIM token generated on the provider's page in Pando.",
		}},
		"meta": map[string]any{"resourceType": "ServiceProviderConfig", "location": c.Base + "/ServiceProviderConfig"},
	}
}

// SCIMResourceTypes answers GET /ResourceTypes.
func (s *Service) SCIMResourceTypes(c SCIMContext) map[string]any {
	types := []map[string]any{
		{"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"}, "id": "User", "name": "User",
			"endpoint": "/Users", "schema": SchemaUser,
			"schemaExtensions": []map[string]any{{"schema": SchemaEnterprise, "required": false}},
			"meta":             map[string]any{"resourceType": "ResourceType", "location": c.Base + "/ResourceTypes/User"}},
		{"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"}, "id": "Group", "name": "Group",
			"endpoint": "/Groups", "schema": SchemaGroup,
			"meta": map[string]any{"resourceType": "ResourceType", "location": c.Base + "/ResourceTypes/Group"}},
	}
	return listResponse(types, len(types), 0)
}

// SCIMSchemas answers GET /Schemas with the attributes Pando reads. Others a
// client sends are kept and returned, but not described.
func (s *Service) SCIMSchemas(c SCIMContext) map[string]any {
	attr := func(name, typ string, required bool, uniqueness string) map[string]any {
		return map[string]any{"name": name, "type": typ, "multiValued": false, "required": required,
			"caseExact": false, "mutability": "readWrite", "returned": "default", "uniqueness": uniqueness}
	}
	user := map[string]any{
		"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"}, "id": SchemaUser, "name": "User",
		"attributes": []map[string]any{
			attr("userName", "string", true, "server"), attr("externalId", "string", false, "none"),
			attr("displayName", "string", false, "none"), attr("active", "boolean", false, "none"),
			{"name": "name", "type": "complex", "multiValued": false, "required": false, "subAttributes": []map[string]any{
				attr("formatted", "string", false, "none"), attr("givenName", "string", false, "none"),
				attr("familyName", "string", false, "none")}},
			{"name": "emails", "type": "complex", "multiValued": true, "required": false, "subAttributes": []map[string]any{
				attr("value", "string", false, "none"), attr("type", "string", false, "none"),
				attr("primary", "boolean", false, "none")}},
		},
		"meta": map[string]any{"resourceType": "Schema", "location": c.Base + "/Schemas/" + SchemaUser},
	}
	group := map[string]any{
		"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"}, "id": SchemaGroup, "name": "Group",
		"attributes": []map[string]any{
			attr("displayName", "string", true, "none"), attr("externalId", "string", false, "none"),
			{"name": "members", "type": "complex", "multiValued": true, "required": false, "subAttributes": []map[string]any{
				attr("value", "string", false, "none"), attr("display", "string", false, "none")}},
		},
		"meta": map[string]any{"resourceType": "Schema", "location": c.Base + "/Schemas/" + SchemaGroup},
	}
	return listResponse([]map[string]any{user, group}, 2, 0)
}
