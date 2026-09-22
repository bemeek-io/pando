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
