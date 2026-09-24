package state

// withoutNUL removes every NUL character from encoded JSON.
//
// Postgres's jsonb refuses the escape `\u0000`, and Go writes a NUL in any
// string that way. A detection carries text read out of a repository — a
// Dockerfile's CMD in its evidence, a carried file, a trial's log — and one NUL
// in any of it made the whole detection fail to save (issue #55). Removing the
// character loses nothing a person could read.
//
// It reads escapes rather than searching for the six bytes, so a string that
// holds a backslash followed by "u0000" — encoded `\\u0000` — is kept.
func withoutNUL(encoded []byte) []byte {
	out := encoded[:0:0]
	for i := 0; i < len(encoded); i++ {
		c := encoded[i]
		if c != '\\' || i+1 >= len(encoded) {
			out = append(out, c)
			continue
		}
		if encoded[i+1] == 'u' && i+5 < len(encoded) && string(encoded[i+2:i+6]) == "0000" {
			i += 5
			continue
		}
		out = append(out, c, encoded[i+1])
		i++
	}
	return out
}
