// Package docker provisions services that fill declared slots: postgres, mysql, redis.
//
// A provisioned service joins the app's private bundle. It is not exposed, not addressable
// from outside, and not shareable with another app (R-134) — sharing is expressed as two apps
// binding to one external target.
package docker
