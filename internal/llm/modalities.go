package llm

import (
	"context"
	"net/http"
	"slices"
	"sync"
)

// inputModalities remembers what each model reads, as the server's /models
// says it: llama-server lists text, image, audio and video there. A model
// the server says nothing of maps to nil.
type inputModalities struct {
	mu     sync.Mutex
	models map[string][]string
}

// takenMedia is the request's messages with the media the server says its
// model does not read turned into a line saying so. A server that does not
// say gets them all.
func (p *OpenAI) takenMedia(ctx context.Context, req Request) []Message {
	if !slices.ContainsFunc(req.Messages, func(message Message) bool { return len(message.Media) > 0 }) {
		return req.Messages
	}
	reads := p.readsOf(ctx, req.Model)
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

func (p *OpenAI) readsOf(ctx context.Context, model string) []string {
	p.modalities.mu.Lock()
	defer p.modalities.mu.Unlock()
	if reads, known := p.modalities.models[model]; known {
		return reads
	}
	var list struct {
		Data []struct {
			ID           string `json:"id"`
			Architecture *struct {
				InputModalities []string `json:"input_modalities"`
			} `json:"architecture"`
		} `json:"data"`
	}
	if p.getJSON(ctx, http.MethodGet, p.url("/models"), nil, &list) != nil {
		return nil
	}
	var reads []string
	for _, entry := range list.Data {
		listed := entry.ID == model || len(list.Data) == 1
		if listed && entry.Architecture != nil {
			reads = entry.Architecture.InputModalities
		}
	}
	if p.modalities.models == nil {
		p.modalities.models = map[string][]string{}
	}
	p.modalities.models[model] = reads
	return reads
}
