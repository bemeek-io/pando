// Package loopback implements port-mode routing with no TLS: the laptop default.
//
// Like every routing adapter, it makes traffic arrive at Pando's proxy and never reaches the
// workload (R-023).
package loopback
