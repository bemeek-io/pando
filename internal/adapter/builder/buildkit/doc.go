// Package buildkit implements the builder adapter: rootless, containerized, no socket.
//
// A builder must never receive or request a container runtime socket (R-112). The type system
// cannot enforce this; an integration test asserting the build container's mount list does.
package buildkit
