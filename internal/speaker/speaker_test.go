package speaker

// Test Traceability Table
// | ID    | Category | Description                                                         | Priority |
// | SP-01 | Happy    | WAV wraps PCM in a header that says mono 16-bit at the rate given    | P0       |

import (
	"encoding/binary"
	"testing"
)

func TestWAV(t *testing.T) {
	t.Run("SP-01 WAV wraps PCM in a header that says mono 16-bit at the rate given", func(t *testing.T) {
		pcm := []byte{1, 2, 3, 4}

		wav := WAV(pcm, 24000)

		channels, rate, bits := binary.LittleEndian.Uint16(wav[22:]), binary.LittleEndian.Uint32(wav[24:]), binary.LittleEndian.Uint16(wav[34:])
		if string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" || channels != 1 || rate != 24000 || bits != 16 {
			t.Errorf("header % x", wav[:44])
		}
		if size := binary.LittleEndian.Uint32(wav[40:]); size != 4 || string(wav[44:]) != string(pcm) || len(wav) != 48 {
			t.Errorf("data size %d, wav % x", size, wav)
		}
	})
}
