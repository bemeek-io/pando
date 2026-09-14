package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/cli"
)

// isolateConfig points XDG_CONFIG_HOME at a temporary directory, so a test
// never reads or writes the developer's real credentials.
func isolateConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return filepath.Join(dir, "pando", "credentials.json")
}

func TestCredentialsRoundTrip(t *testing.T) {
	isolateConfig(t)

	_, err := cli.LoadCredentials()
	require.ErrorIs(t, err, cli.ErrNotLoggedIn, "no credential yet is not a read failure")

	want := cli.Credentials{URL: "https://pando.example", Token: "tok_01HQ8", User: "ben"}
	require.NoError(t, cli.SaveCredentials(want))

	got, err := cli.LoadCredentials()
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// A token is a bearer credential: anything that can read the file can act as
// the person who created it.
func TestTheCredentialFileIsReadableOnlyByItsOwner(t *testing.T) {
	path := isolateConfig(t)
	require.NoError(t, cli.SaveCredentials(cli.Credentials{URL: "https://pando.example", Token: "tok_01HQ8"}))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	dir, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), dir.Mode().Perm())
}

func TestACorruptCredentialFileSaysWhichFile(t *testing.T) {
	path := isolateConfig(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))

	_, err := cli.LoadCredentials()
	require.ErrorContains(t, err, path)
}

func TestNewRequiresEitherACredentialOrAServerURL(t *testing.T) {
	isolateConfig(t)

	_, err := cli.New("")
	require.ErrorIs(t, err, cli.ErrNotLoggedIn)

	// A URL override works without logging in, so a script can point at another
	// install.
	c, err := cli.New("https://other.example/")
	require.NoError(t, err)
	require.Equal(t, "https://other.example", c.BaseURL, "the trailing slash is trimmed")
	require.Empty(t, c.Token)
}

func TestNewPrefersTheOverrideOverTheStoredURL(t *testing.T) {
	isolateConfig(t)
	require.NoError(t, cli.SaveCredentials(cli.Credentials{URL: "https://stored.example", Token: "tok_01HQ8"}))

	c, err := cli.New("")
	require.NoError(t, err)
	require.Equal(t, "https://stored.example", c.BaseURL)
	require.Equal(t, "tok_01HQ8", c.Token)

	c, err = cli.New("https://override.example")
	require.NoError(t, err)
	require.Equal(t, "https://override.example", c.BaseURL)
	require.Equal(t, "tok_01HQ8", c.Token, "the stored token still applies")
}

func TestNewRejectsAStoredCredentialWithNoURL(t *testing.T) {
	isolateConfig(t)
	require.NoError(t, cli.SaveCredentials(cli.Credentials{Token: "tok_01HQ8"}))

	_, err := cli.New("")
	require.ErrorIs(t, err, cli.ErrNotLoggedIn)
}

// recorder captures what the client sent, so a test can assert the request
// rather than only the response handling.
type recorder struct {
	method, path, auth, contentType string
	body                            []byte
}

func serve(t *testing.T, status int, response string) (*cli.Client, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.method, rec.path = r.Method, r.URL.RequestURI()
		rec.auth = r.Header.Get("Authorization")
		rec.contentType = r.Header.Get("Content-Type")
		rec.body, _ = io.ReadAll(r.Body)

		w.WriteHeader(status)
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(srv.Close)

	return &cli.Client{BaseURL: srv.URL, Token: "tok_01HQ8", HTTP: srv.Client()}, rec
}

func TestDoSendsTheTokenAndTheAPIPrefix(t *testing.T) {
	c, rec := serve(t, http.StatusOK, `{"id":"app_01HQ8"}`)

	var out map[string]string
	require.NoError(t, c.Do("POST", "/apps", map[string]string{"name": "notes"}, &out))

	require.Equal(t, "POST", rec.method)
	require.Equal(t, "/api/v1/apps", rec.path)
	require.Equal(t, "Bearer tok_01HQ8", rec.auth)
	require.Equal(t, "application/json", rec.contentType)
	require.JSONEq(t, `{"name":"notes"}`, string(rec.body))
	require.Equal(t, map[string]string{"id": "app_01HQ8"}, out)
}

func TestDoSendsNoBodyOrContentTypeWhenThereIsNothingToSend(t *testing.T) {
	c, rec := serve(t, http.StatusOK, `{}`)
	require.NoError(t, c.Do("GET", "/apps", nil, nil))

	require.Empty(t, rec.body)
	require.Empty(t, rec.contentType)
}

func TestAnAnonymousClientSendsNoAuthorization(t *testing.T) {
	c, rec := serve(t, http.StatusOK, `{}`)
	c.Token = ""
	require.NoError(t, c.Do("GET", "/apps", nil, nil))
	require.Empty(t, rec.auth)
}

func TestDoDiscardsTheBodyWhenThereIsNothingToDecodeInto(t *testing.T) {
	c, _ := serve(t, http.StatusOK, `{"ignored":true}`)
	require.NoError(t, c.Do("DELETE", "/tokens/tok_01HQ8", nil, nil))
}

func TestDoDoesNotDecodeA204(t *testing.T) {
	c, _ := serve(t, http.StatusNoContent, "")
	var out map[string]any
	require.NoError(t, c.Do("DELETE", "/sessions", nil, &out))
	require.Nil(t, out, "there was no body, and none was invented")
}

// The server's messages are held to the R-105 standard. Rendering them as
// written is the point: paraphrasing here would undo that at the last step.
func TestR105_TheServersErrorEnvelopeIsRenderedAsWritten(t *testing.T) {
	c, _ := serve(t, http.StatusConflict, `{
		"code":"PLAN_SLOT_UNFILLED",
		"message":"This app needs a Redis, and one hasn't been chosen yet.",
		"remedy":"Run pando slot set notes REDIS_URL --provision.",
		"request_id":"req_01HQ8"
	}`)

	err := c.Do("POST", "/apps/notes/deployments", map[string]any{}, nil)

	var apiErr *cli.APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusConflict, apiErr.Status)
	require.Equal(t, "PLAN_SLOT_UNFILLED", apiErr.Code)
	require.Equal(t, "req_01HQ8", apiErr.ReqID)
	require.Equal(t,
		"This app needs a Redis, and one hasn't been chosen yet.\nRun pando slot set notes REDIS_URL --provision.",
		err.Error(), "the remedy is printed under the message")
}

func TestAnErrorWithNoRemedyPrintsOnlyTheMessage(t *testing.T) {
	c, _ := serve(t, http.StatusNotFound, `{"code":"NOT_FOUND","message":"There is no app with that ID."}`)
	err := c.Do("GET", "/apps/app_missing", nil, nil)
	require.EqualError(t, err, "There is no app with that ID.")
}

// A proxy or a crash can return something that is not an envelope at all. The
// status is still worth reporting rather than an empty message.
func TestAResponseThatIsNotAnEnvelopeStillReportsTheStatus(t *testing.T) {
	for _, body := range []string{"<html>502 Bad Gateway</html>", "", "{}"} {
		c, _ := serve(t, http.StatusBadGateway, body)
		err := c.Do("GET", "/apps", nil, nil)
		require.EqualError(t, err, "The server returned 502.")
	}
}

func TestAnUnreachableServerSaysWhichServer(t *testing.T) {
	c := &cli.Client{BaseURL: "http://127.0.0.1:1", HTTP: &http.Client{}}

	err := c.Do("GET", "/apps", nil, nil)
	require.ErrorContains(t, err, "http://127.0.0.1:1")

	_, err = c.Stream("GET", "/apps/notes/logs", nil)
	require.ErrorContains(t, err, "http://127.0.0.1:1")

	require.ErrorContains(t, c.UploadSource("app_01HQ8", []byte("x")), "http://127.0.0.1:1")
}

func TestABodyThatCannotBeEncodedIsReportedBeforeTheRequest(t *testing.T) {
	c, _ := serve(t, http.StatusOK, `{}`)
	unencodable := map[string]any{"fn": func() {}}

	require.Error(t, c.Do("POST", "/apps", unencodable, nil))

	_, err := c.Stream("POST", "/apps", unencodable)
	require.Error(t, err)
}

func TestStreamReturnsTheBodyUnbuffered(t *testing.T) {
	c, rec := serve(t, http.StatusOK, "line one\nline two\n")

	body, err := c.Stream("GET", "/apps/notes/logs?follow=true", nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, body.Close()) }()

	require.Equal(t, "/api/v1/apps/notes/logs?follow=true", rec.path)
	require.Equal(t, "Bearer tok_01HQ8", rec.auth)

	got, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, "line one\nline two\n", string(got))
}

func TestStreamReportsTheServersError(t *testing.T) {
	c, _ := serve(t, http.StatusForbidden,
		`{"code":"PERM_DENIED","message":"You cannot read this app's logs."}`)

	_, err := c.Stream("GET", "/apps/notes/logs", nil)
	var apiErr *cli.APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, "PERM_DENIED", apiErr.Code)

	c, _ = serve(t, http.StatusInternalServerError, "not an envelope")
	_, err = c.Stream("GET", "/apps/notes/logs", nil)
	require.EqualError(t, err, "The server returned 500.")
}

func TestStreamSendsABodyWhenGivenOne(t *testing.T) {
	c, rec := serve(t, http.StatusOK, "ok")
	body, err := c.Stream("POST", "/apps/notes/logs", map[string]string{"since": "1h"})
	require.NoError(t, err)
	require.NoError(t, body.Close())
	require.JSONEq(t, `{"since":"1h"}`, string(rec.body))
}

func TestUploadSourceSendsTheArchiveAsGzip(t *testing.T) {
	c, rec := serve(t, http.StatusAccepted, `{}`)
	require.NoError(t, c.UploadSource("app_01HQ8", []byte("archive bytes")))

	require.Equal(t, "POST", rec.method)
	require.Equal(t, "/api/v1/apps/app_01HQ8/source", rec.path)
	require.Equal(t, "application/gzip", rec.contentType)
	require.Equal(t, "Bearer tok_01HQ8", rec.auth)
	require.Equal(t, "archive bytes", string(rec.body))
}

// An upload is the one call where the server may answer with a plain-text limit
// message rather than an envelope, so the text is kept rather than replaced.
func TestUploadSourceKeepsANonEnvelopeBody(t *testing.T) {
	c, _ := serve(t, http.StatusRequestEntityTooLarge, "that upload is larger than this install allows")
	err := c.UploadSource("app_01HQ8", []byte("x"))
	require.EqualError(t, err, "that upload is larger than this install allows")

	c, _ = serve(t, http.StatusRequestEntityTooLarge, "")
	require.EqualError(t, c.UploadSource("app_01HQ8", []byte("x")), "The server returned 413.")

	c, _ = serve(t, http.StatusBadRequest, `{"code":"VALID_INVALID","message":"That is not a gzip archive."}`)
	require.EqualError(t, c.UploadSource("app_01HQ8", []byte("x")), "That is not a gzip archive.")
}

func TestUploadSourceSendsNoAuthorizationWithoutAToken(t *testing.T) {
	c, rec := serve(t, http.StatusOK, `{}`)
	c.Token = ""
	require.NoError(t, c.UploadSource("app_01HQ8", []byte("x")))
	require.Empty(t, rec.auth)
}

func TestAMalformedMethodIsReportedRatherThanSent(t *testing.T) {
	c, _ := serve(t, http.StatusOK, `{}`)

	require.Error(t, c.Do("not a method\n", "/apps", nil, nil))
	_, err := c.Stream("not a method\n", "/apps", nil)
	require.Error(t, err)
	require.Error(t, (&cli.Client{BaseURL: "://bad", HTTP: http.DefaultClient}).
		UploadSource("app_01HQ8", []byte("x")))
}

func TestAResponseThatIsNotTheExpectedShapeIsADecodeError(t *testing.T) {
	c, _ := serve(t, http.StatusOK, `{"apps": "not an array"}`)

	var out struct {
		Apps []string `json:"apps"`
	}
	require.Error(t, c.Do("GET", "/apps", nil, &out))
}

func TestAPIErrorIsJSONDecodable(t *testing.T) {
	// The envelope's field names are the server's, and the CLI decodes them
	// verbatim — a rename on either side has to break here.
	var e cli.APIError
	require.NoError(t, json.Unmarshal([]byte(
		`{"code":"C","message":"M","remedy":"R","request_id":"req_1"}`), &e))
	require.Equal(t, cli.APIError{Code: "C", Message: "M", Remedy: "R", ReqID: "req_1"}, e)
}
