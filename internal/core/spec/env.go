package spec

// SetEnv sets a variable a person chose, on the workloads it belongs to.
//
// With a workload named, that workload only. Without one, every workload that
// already declares the key — a variable detection found in .env.example is
// usually read by one part, and naming the part is something the person should
// not have to know. A key no workload declares goes to the primary one: it is
// a variable the person is adding, and the primary workload is the app.
//
// The entry replaces any existing one with the key, and is marked as the
// person's (EnvFromUser), so it outlives a re-detection (R-022).
func SetEnv(s *AppSpec, workload, key string, entry EnvEntry) {
	entry.Key = key
	entry.Source = EnvFromUser

	set := func(w *Workload) {
		for i := range w.Env {
			if w.Env[i].Key == key {
				w.Env[i] = entry
				return
			}
		}
		w.Env = append(w.Env, entry)
	}

	if workload != "" {
		for i := range s.Workloads {
			if s.Workloads[i].Name == workload {
				set(&s.Workloads[i])
			}
		}
		return
	}

	declared := false
	for i := range s.Workloads {
		for _, e := range s.Workloads[i].Env {
			if e.Key == key {
				set(&s.Workloads[i])
				declared = true
				break
			}
		}
	}
	if declared {
		return
	}
	for i := range s.Workloads {
		if s.Workloads[i].Primary {
			set(&s.Workloads[i])
			return
		}
	}
	if len(s.Workloads) > 0 {
		set(&s.Workloads[0])
	}
}

// SlotSecretKey is the secret a literal slot value is stored under. The same
// name PUT /slots/{key} writes, so a value set during review and one set later
// on the settings screen are one value.
func SlotSecretKey(slot string) string { return "slot_" + slot }

// EnvSlot returns the slot a variable is filled from, when it is: the SlotRef
// on the entry with this key, on the named workload or, with none named, on
// any workload.
func EnvSlot(s *AppSpec, workload, key string) (string, bool) {
	for _, w := range s.Workloads {
		if workload != "" && w.Name != workload {
			continue
		}
		for _, e := range w.Env {
			if e.Key == key && e.SlotRef != nil {
				return *e.SlotRef, true
			}
		}
	}
	return "", false
}

// FillSlotLiteral fills a slot with a value somebody supplied, stored as the
// secret named secretRef (R-132).
//
// A variable filled from a slot was given a value by replacing its entry,
// which dropped the SlotRef and left the slot itself unfilled — and a required
// unfilled slot refuses the deploy, so a value typed during review did
// nothing but move the refusal to deploy time. The value belongs on the slot.
// A service slot filled this way stops being provisioned: the person has said
// what the app should connect to.
func FillSlotLiteral(s *AppSpec, slot, secretRef string) bool {
	for i := range s.Slots {
		if s.Slots[i].Key == slot {
			s.Slots[i].Resolution = &Resolution{Mode: ResolutionLiteral, SecretRef: secretRef}
			return true
		}
	}
	return false
}
