package agent

type BudgetItem struct {
	Name   string
	Tokens int
}

// Budget estimates what every request carries before any message: the system
// prompt and each tool definition the model sees, in that order.
func (a *Agent) Budget() []BudgetItem {
	items := []BudgetItem{{Name: "system prompt", Tokens: len(a.System) / bytesPerTokenGuess}}
	for _, spec := range a.Tools.Specs(a.History) {
		size := len(spec.Name) + len(spec.Description) + len(spec.Schema)
		items = append(items, BudgetItem{Name: "tool " + spec.Name, Tokens: size / bytesPerTokenGuess})
	}
	return items
}
