package agent

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | MD-01 | Happy    | An audio or video file named in a prompt goes with it                 | P0       |
// | MD-02 | Happy    | The audio and video a read marks reach the model after the results   | P0       |
// | MD-03 | Edge     | A recording over the limit, or a file of no known kind, is not loaded | P1       |

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/render"
)

func mediaFile(t *testing.T, name string, size int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMediaAttach(t *testing.T) {
	t.Run("MD-01 an audio or video file named in a prompt goes with it", func(t *testing.T) {
		agent, _ := answered(t)
		voice, clip := mediaFile(t, "voice.mp3", 10), mediaFile(t, "clip.webm", 10)

		message := agent.promptMessage("hear "+voice+" and see "+clip, Hooks{})

		if len(message.Media) != 2 || message.Media[0].MIME != "audio/mpeg" || message.Media[1].MIME != "video/webm" {
			t.Errorf("media = %+v", message.Media)
		}
	})

	t.Run("MD-02 the audio and video a read marks reach the model after the results", func(t *testing.T) {
		voice := mediaFile(t, "voice.wav", 10)
		calls := []llm.ToolCall{{ID: "r", Name: "read", Args: json.RawMessage(`{}`)}}
		results := []llm.Message{{Role: llm.RoleTool, ToolCallID: "r", Text: render.MediaMarker("audio", "wav, 0 KB", voice)}}

		note, found := toolMedia(calls, results)

		if !found || len(note.Media) != 1 || note.Media[0].Kind() != "audio" || !strings.Contains(note.Text, "voice.wav") {
			t.Errorf("note = %+v", note)
		}
	})

	t.Run("MD-03 a recording over the limit, or a file of no known kind, is not loaded", func(t *testing.T) {
		for _, path := range []string{mediaFile(t, "long.mp4", largestRecording+1), mediaFile(t, "notes.txt", 10)} {
			if _, err := loadMedia(path); err == nil {
				t.Errorf("%s was loaded", filepath.Base(path))
			}
		}
	})
}
