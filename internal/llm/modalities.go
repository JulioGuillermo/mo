package llm

import (
	"context"
	"net/http"
	"slices"
	"sync"
)

// modelModalities remembers what each model reads and writes, as the
// server's /models says it: llama-server lists text, image, audio and video
// there. A model the server says nothing of has neither.
type modelModalities struct {
	mu     sync.Mutex
	models map[string]modalities
}

type modalities struct {
	reads, writes []string
}

// takenMedia is the request's messages with the media the server says its
// model does not read turned into a line saying so. A server that does not
// say gets them all.
func (p *OpenAI) takenMedia(ctx context.Context, req Request) []Message {
	if !slices.ContainsFunc(req.Messages, func(message Message) bool { return len(message.Media) > 0 }) {
		return req.Messages
	}
	reads := p.modalitiesOf(ctx, req.Model).reads
	if reads == nil {
		return req.Messages
	}
	messages := slices.Clone(req.Messages)
	for i, message := range messages {
		var kept []Media
		for _, media := range message.Media {
			if slices.Contains(reads, media.Kind()) {
				kept = append(kept, media)
				continue
			}
			messages[i].Text += "\n" + media.notTaken()
		}
		messages[i].Media = kept
	}
	return messages
}

// Speaks tells whether the server says the model writes audio.
func (p *OpenAI) Speaks(ctx context.Context, model string) bool {
	return slices.Contains(p.modalitiesOf(ctx, model).writes, "audio")
}

func (p *OpenAI) modalitiesOf(ctx context.Context, model string) modalities {
	p.modalities.mu.Lock()
	defer p.modalities.mu.Unlock()
	if known, found := p.modalities.models[model]; found {
		return known
	}
	var list struct {
		Data []struct {
			ID           string `json:"id"`
			Architecture *struct {
				InputModalities  []string `json:"input_modalities"`
				OutputModalities []string `json:"output_modalities"`
			} `json:"architecture"`
		} `json:"data"`
	}
	if p.getJSON(ctx, http.MethodGet, p.url("/models"), nil, &list) != nil {
		return modalities{}
	}
	var found modalities
	for _, entry := range list.Data {
		listed := entry.ID == model || len(list.Data) == 1
		if listed && entry.Architecture != nil {
			found = modalities{reads: entry.Architecture.InputModalities, writes: entry.Architecture.OutputModalities}
		}
	}
	if p.modalities.models == nil {
		p.modalities.models = map[string]modalities{}
	}
	p.modalities.models[model] = found
	return found
}
