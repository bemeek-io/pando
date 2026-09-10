// Package proxy is the identity-aware reverse proxy: the single enforcement point for every
// request to every app (R-023).
//
// There is no bypass — not for public apps, not for performance, not for websockets. Inbound
// X-Pando-* headers are stripped unconditionally before assertion headers are set; without
// that, a client sets X-Pando-User and any app trusting the convenience headers is trivially
// spoofed (R-053). Read the package CLAUDE.md before changing anything here.
package proxy
