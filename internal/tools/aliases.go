package tools

// aliases are the names tools had, which still name them: configuration,
// permission rules and conversations written before the change keep working.
var aliases = map[string]string{"fetch": "web_fetch"}

// Canonical is the name a tool has now, given one it had or has.
func Canonical(name string) string {
	if current, renamed := aliases[name]; renamed {
		return current
	}
	return name
}
