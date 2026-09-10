// Package assertion mints the identity assertions the proxy passes to apps, and publishes
// the JWKS that verifies them.
//
// Ed25519, aud bound to the app ID to prevent cross-app replay, 120s lifetime, minted per
// request (R-051, R-055). The sub claim is users.id, stable across email change and
// independent of the identity adapter — apps key their data on it (R-054).
package assertion
