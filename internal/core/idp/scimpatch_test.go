package idp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func ops(t *testing.T, body string) []PatchOp {
	t.Helper()
	var req PatchRequest
	require.NoError(t, json.Unmarshal([]byte(body), &req))
	return req.Operations
}

func TestParseFilter(t *testing.T) {
	cases := map[string]Filter{
		`userName eq "dana@example.com"`:                             {Attr: "userName", Value: "dana@example.com"},
		`USERNAME EQ "Dana"`:                                         {Attr: "userName", Value: "Dana"},
		`externalId eq "0a21f0f2-8d2a"`:                              {Attr: "externalId", Value: "0a21f0f2-8d2a"},
		`emails[type eq "work"].value eq "a@b.c"`:                    {Attr: "emails.value", Value: "a@b.c"},
		`urn:ietf:params:scim:schemas:core:2.0:User:userName eq "x"`: {Attr: "userName", Value: "x"},
		`userName eq "quote \" inside"`:                              {Attr: "userName", Value: `quote " inside`},
		``:                                                           {},
	}
	for raw, want := range cases {
		got, err := ParseFilter(raw, "userName", "externalId", "emails.value")
		require.NoError(t, err, raw)
		require.Equal(t, want, got, raw)
	}
	for _, bad := range []string{`title co "x"`, `userName eq x`, `userName eq "a" and externalId eq "b"`, `displayName eq "x"`} {
		_, err := ParseFilter(bad, "userName", "externalId")
		var se *SCIMError
		require.ErrorAs(t, err, &se, bad)
		require.Equal(t, "invalidFilter", se.Type)
	}
}

// Okta replaces with a value object and no path.
func TestApplyPatchOktaShape(t *testing.T) {
	r := map[string]any{"userName": "dana", "active": true}
	require.NoError(t, ApplyPatch(r, ops(t, `{"Operations":[{"op":"replace","value":{"active":false}}]}`)))
	require.Equal(t, false, r["active"])
}

// Entra capitalizes ops, sends a string boolean, and addresses emails and the
// enterprise extension by path.
func TestApplyPatchEntraShape(t *testing.T) {
	r := map[string]any{
		"userName": "u", "active": true,
		"emails": []any{map[string]any{"type": "work", "value": "old@x.com", "primary": true}},
		"name":   map[string]any{"givenName": "g"},
	}
	require.NoError(t, ApplyPatch(r, ops(t, `{"Operations":[
		{"op":"Replace","path":"emails[type eq \"work\"].value","value":"new@x.com"},
		{"op":"Add","path":"emails[type eq \"home\"].value","value":"home@x.com"},
		{"op":"Replace","path":"name.familyName","value":"f"},
		{"op":"Add","path":"urn:ietf:params:scim:schemas:extension:enterprise:2.0:User:department","value":"R&D"},
		{"op":"Replace","path":"urn:ietf:params:scim:schemas:core:2.0:User:displayName","value":"Dana"},
		{"op":"Replace","path":"active","value":"False"}
	]}`)))
	emails := r["emails"].([]any)
	require.Equal(t, "new@x.com", emails[0].(map[string]any)["value"])
	require.Equal(t, "home@x.com", emails[1].(map[string]any)["value"])
	require.Equal(t, "f", r["name"].(map[string]any)["familyName"])
	require.Equal(t, "g", r["name"].(map[string]any)["givenName"])
	require.Equal(t, "R&D", r["urn:ietf:params:scim:schemas:extension:enterprise:2.0:User"].(map[string]any)["department"])
	require.Equal(t, "Dana", r["displayName"])
	active, err := boolValue(r["active"], true)
	require.NoError(t, err)
	require.False(t, active)
}

func TestApplyPatchRemove(t *testing.T) {
	r := map[string]any{"title": "x", "emails": []any{
		map[string]any{"type": "work", "value": "a"}, map[string]any{"type": "home", "value": "b"},
	}}
	require.NoError(t, ApplyPatch(r, ops(t, `{"Operations":[
		{"op":"remove","path":"title"},
		{"op":"remove","path":"emails[type eq \"home\"]"}
	]}`)))
	require.NotContains(t, r, "title")
	require.Len(t, r["emails"], 1)

	err := ApplyPatch(r, ops(t, `{"Operations":[{"op":"remove"}]}`))
	var se *SCIMError
	require.ErrorAs(t, err, &se)
	require.Equal(t, "noTarget", se.Type)

	err = ApplyPatch(r, ops(t, `{"Operations":[{"op":"move","path":"x"}]}`))
	require.ErrorAs(t, err, &se)
}

func TestMemberOps(t *testing.T) {
	add, remove, clear, rest, err := MemberOps(ops(t, `{"Operations":[
		{"op":"Add","path":"members","value":[{"value":"usr_a"},{"value":"usr_b"}]},
		{"op":"Remove","path":"members[value eq \"usr_c\"]"},
		{"op":"remove","path":"members","value":[{"value":"usr_d"}]},
		{"op":"Replace","path":"displayName","value":"New"}
	]}`))
	require.NoError(t, err)
	require.Equal(t, []string{"usr_a", "usr_b"}, add)
	require.Equal(t, []string{"usr_c", "usr_d"}, remove)
	require.False(t, clear)
	require.Len(t, rest, 1)

	// Okta: replace the whole membership with a value object.
	add, _, clear, rest, err = MemberOps(ops(t, `{"Operations":[
		{"op":"replace","value":{"id":"grp_1","displayName":"Eng","members":[{"value":"usr_x"}]}}
	]}`))
	require.NoError(t, err)
	require.True(t, clear)
	require.Equal(t, []string{"usr_x"}, add)
	require.Len(t, rest, 1)
	require.NotContains(t, string(rest[0].Value), "members")

	// Remove with no value empties the group.
	_, _, clear, _, err = MemberOps(ops(t, `{"Operations":[{"op":"remove","path":"members"}]}`))
	require.NoError(t, err)
	require.True(t, clear)
}

func TestBoolValue(t *testing.T) {
	for in, want := range map[any]bool{true: true, false: false, "True": true, "FALSE": false, nil: true} {
		got, err := boolValue(in, true)
		require.NoError(t, err)
		require.Equal(t, want, got, in)
	}
	_, err := boolValue("maybe", true)
	require.Error(t, err)
}

func TestPaging(t *testing.T) {
	o, l := Paging(0, 0)
	require.Equal(t, 0, o)
	require.Equal(t, 100, l)
	o, l = Paging(11, 10)
	require.Equal(t, 10, o)
	require.Equal(t, 10, l)
	_, l = Paging(1, 5000)
	require.Equal(t, 200, l)
}

// TestSafeNextKeepsASignInOnThisHost asserts that where a sign-in lands is a
// path, never a URL — an open redirect on a sign-in page sends people to a
// look-alike that asks for their password again.
func TestSafeNextKeepsASignInOnThisHost(t *testing.T) {
	for in, want := range map[string]string{
		"":                       "/",
		"/apps/notes?x=1":        "/apps/notes?x=1",
		"//evil.example":         "/",
		"/\\evil.example":        "/",
		"https://evil.example/":  "/",
		"javascript:alert(1)":    "/",
		"/ok\r\nSet-Cookie: x=y": "/",
	} {
		require.Equal(t, want, SafeNext(in), in)
	}
}

func TestSCIMErrorBody(t *testing.T) {
	b := (&SCIMError{Status: 409, Type: "uniqueness", Detail: "taken"}).Body()
	require.Equal(t, "409", b["status"])
	require.Equal(t, "uniqueness", b["scimType"])
	require.Equal(t, []string{SchemaError}, b["schemas"])
}
