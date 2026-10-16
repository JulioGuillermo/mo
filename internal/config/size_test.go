package config

// Test Traceability Table
// | ID    | Category | Description                                                   | Priority |
// | SZ-01 | Edge     | Sizes read as 10MB, 512kb, 1.5GB or bytes; 0 is no limit      | P1       |
// | SZ-02 | Error    | A read_max_size that is no size is refused when loading       | P1       |

import "testing"

func TestSizes(t *testing.T) {
	t.Run("SZ-01 sizes read as 10MB, 512kb, 1.5GB or bytes; 0 is no limit", func(t *testing.T) {
		cases := map[string]int64{"10MB": 10 << 20, "512kb": 512 << 10, "1.5GB": 3 << 29, "2048": 2048, "0": 0, "": 0, "3 M": 3 << 20}
		for text, want := range cases {
			if got, err := ParseSize(text); err != nil || got != want {
				t.Errorf("%q = %d, %v", text, got, err)
			}
		}
		if _, err := ParseSize("lots"); err == nil {
			t.Error("lots read as a size")
		}
	})

	t.Run("SZ-02 a read_max_size that is no size is refused when loading", func(t *testing.T) {
		cfg := Defaults()
		cfg.Tools.ReadMaxSize = "big"

		if err := validate(cfg); err == nil {
			t.Error("big accepted")
		}
	})
}
