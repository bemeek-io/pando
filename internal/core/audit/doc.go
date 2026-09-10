// Package audit writes the append-only event log.
//
// Append-only is enforced at the database level: the application role holds INSERT on
// audit_events and neither UPDATE nor DELETE (R-027). Every authorization denial is audited,
// not only successes, and the event is written before the privileged action, not after —
// an exec session that fails to open is still recorded as attempted (R-228).
package audit
