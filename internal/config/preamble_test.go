package config

import "testing"

func TestPreambleMerge(t *testing.T) {
	t.Run("preamble = false on the antigravity preset keeps the rest of the preset", func(t *testing.T) {
		off := false
		merged := UserSettings(Config{Providers: map[string]ProviderConfig{"antigravity": {Preamble: &off}}}).Providers["antigravity"]

		if merged.Preamble == nil || *merged.Preamble || merged.Protocol != "cloudcode" || merged.OAuth == nil {
			t.Errorf("merged = %+v", merged)
		}
	})
}
