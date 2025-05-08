// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the AGPL v3
// that can be found in the LICENSE file.

package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/maruel/citygpt/internal/htmlparse"
	"github.com/maruel/genai"
)

// Index is the content of index.json.
type Index struct {
	Version  int       `json:"version"`
	Created  time.Time `json:"created"`
	Modified time.Time `json:"modified"`
	Items    []Item    `json:"items"`
}

// Item is one indexed item.
type Item struct {
	URL      string    `json:"url"`
	Name     string    `json:"name"`
	Title    string    `json:"title"`
	Summary  string    `json:"summary"`
	Created  time.Time `json:"created"`
	Modified time.Time `json:"modified"`
	Model    string    `json:"model"` // Model used to generate the summary. Use one per item since they can be added incrementally.
}

// Load loads an Index from a filesystem.
func (i *Index) Load(fsys fs.FS, path string) error {
	raw, err := fs.ReadFile(fsys, path)
	if err != nil {
		if os.IsNotExist(err) {
			// If the file doesn't exist, initialize with an empty Index
			*i = Index{Version: 1, Created: time.Now().Round(time.Second), Modified: time.Now().Round(time.Second)}
			return nil
		}
		return err
	}
	return json.Unmarshal(raw, i)
}

// Save saves an Index to a file path.
func (i *Index) Save(path string) error {
	d, err := json.MarshalIndent(i, "", " ")
	if err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(path, d, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

//

const summarizationPrompt = "You are a helpful assistant that summarizes text content accurately and concisely. Do not mention what you are doing or your constraints. Do not mention the city or the fact it is about by-laws. Please summarize the subject of following text as a single long line:"

// Summarize creates a summary of a the provided content.
func Summarize(ctx context.Context, c genai.ChatProvider, content string) (string, error) {
	messages := genai.Messages{
		genai.NewTextMessage(genai.User, summarizationPrompt),
		genai.NewTextMessage(genai.User, content),
	}
	opts := genai.ChatOptions{Seed: 1, Temperature: 0.3, MaxTokens: 1024 * 1024}
	resp, err := c.Chat(ctx, messages, &opts)
	if err != nil {
		return "", err
	}
	return resp.Contents[0].Text, nil
}

// ProcessHTML from a single URL and saves it
func ProcessHTML(ctx context.Context, c genai.ChatProvider, r io.Reader, mdPath string) (string, string, error) {
	md, title, err := htmlparse.ExtractTextFromHTML(r)
	if err != nil {
		return title, "", fmt.Errorf("failed to extract text: %w", err)
	}
	if err = os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		return title, "", fmt.Errorf("failed to write file %s: %w", mdPath, err)
	}
	summary, err := Summarize(ctx, c, md)
	return title, summary, err
}
