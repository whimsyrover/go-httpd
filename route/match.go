package route

type Match struct {
	*Route
	values []string // variable values
}

// Values returns all variable values
func (m Match) Values() []string { return m.values }

// Vars returns all variable names and values as a map
func (m Match) Vars() map[string]string {
	kv := make(map[string]string, len(m.Route.Vars))
	if len(m.Route.Vars) > 0 {
		for name, index := range m.Route.Vars {
			kv[name] = m.values[index]
		}
	}
	return kv
}

// Var retrieves the value of a variable by name
func (m Match) Var(name string, fallback ...string) string {
	if i, ok := m.Route.Vars[name]; ok && m.values != nil {
		return m.values[i]
	}
	if len(fallback) > 0 {
		return fallback[0]
	}
	return ""
}
