package docker

import (
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// execSession adapts Docker's hijacked connection to api.ExecSession.
type execSession struct {
	cli      *client.Client
	execID   string
	hijacked types.HijackedResponse
}

func (s *execSession) Read(p []byte) (int, error)  { return s.hijacked.Reader.Read(p) }
func (s *execSession) Write(p []byte) (int, error) { return s.hijacked.Conn.Write(p) }

func (s *execSession) Close() error {
	s.hijacked.Close()
	return nil
}

func (s *execSession) Resize(rows, cols uint16) error {
	return s.cli.ContainerExecResize(context.Background(), s.execID, container.ResizeOptions{
		Height: uint(rows),
		Width:  uint(cols),
	})
}

// ExitCode reports the session's exit code once it has finished.
//
// The bool is false while the session is still running, so a caller can tell
// "not finished" from "finished with 0" — collapsing those would report success
// for a session still in progress.
func (s *execSession) ExitCode() (int, bool) {
	inspect, err := s.cli.ContainerExecInspect(context.Background(), s.execID)
	if err != nil || inspect.Running {
		return 0, false
	}
	return inspect.ExitCode, true
}
