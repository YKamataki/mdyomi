package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const endpoint = "https://api.openai.com/v1/responses"

type openAI struct {
	client             *http.Client
	key, model, prompt string
}

const readingFormat = `{
  "type": "json_schema", "name": "readings", "strict": true,
  "schema": {
    "type": "object", "additionalProperties": false, "required": ["decisions"],
    "properties": {
      "decisions": {"type": "array", "items": {
        "type": "object", "additionalProperties": false, "required": ["id", "category", "reading"],
        "properties": {
          "id": {"type": "integer"},
          "category": {"type": "string", "enum": ["abbreviation", "command_identifier", "other", "foreign_noun"]},
          "reading": {"type": "string", "description": "Katakana only for foreign_noun. Empty string for all other categories."}
        }
      }}
    }
  }
}`

func (a openAI) readings(ctx context.Context, batch []candidate) ([]edit, error) {
	if strings.TrimSpace(a.key) == "" {
		return nil, errors.New("環境変数 OPENAI_API_KEY を設定してください")
	}
	input, err := json.Marshal(batch)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{
		"model": a.model, "instructions": a.prompt, "input": string(input), "store": false,
		"text": map[string]any{"format": json.RawMessage(readingFormat)},
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.key)
	req.Header.Set("Content-Type", "application/json")
	res, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OpenAI API への接続: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		// Error bodies may quote input text or a credential.
		return nil, fmt.Errorf("OpenAI API: HTTP %d %s", res.StatusCode, http.StatusText(res.StatusCode))
	}
	const limit = 2 << 20
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if len(b) > limit {
		return nil, errors.New("OpenAI API の応答が大きすぎます")
	}
	return decodeReadings(b, len(batch))
}

func decodeReadings(body []byte, count int) ([]edit, error) {
	var res struct {
		Status string
		Output []struct {
			Type    string
			Content []struct{ Type, Text string }
		}
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("OpenAI API の応答を解析できません: %w", err)
	}
	if res.Status != "completed" {
		return nil, fmt.Errorf("OpenAI API の応答が完了していません (status=%q)", res.Status)
	}
	var payload string
	for _, item := range res.Output {
		if item.Type != "message" {
			continue
		}
		for _, part := range item.Content {
			switch part.Type {
			case "refusal":
				return nil, errors.New("モデルが読みの生成を辞退しました")
			case "output_text":
				if payload != "" {
					return nil, errors.New("モデルから複数の出力を受け取りました")
				}
				payload = part.Text
			}
		}
	}
	var result struct {
		Decisions []struct {
			ID                int
			Category, Reading string
		}
	}
	if err := json.Unmarshal([]byte(payload), &result); err != nil || len(result.Decisions) != count {
		return nil, errors.New("モデルの分類結果が候補数と一致しません")
	}
	seen := make([]bool, count)
	var edits []edit
	for _, d := range result.Decisions {
		if d.ID < 1 || d.ID > count || seen[d.ID-1] {
			return nil, fmt.Errorf("分類結果の候補 id=%d が不正です", d.ID)
		}
		seen[d.ID-1] = true
		switch d.Category {
		case "foreign_noun":
			edits = append(edits, edit{d.ID, d.Reading})
		case "abbreviation", "command_identifier", "other":
			// Preserve the source spelling even if a reading was supplied.
		default:
			return nil, errors.New("モデルが未定義の分類を返しました")
		}
	}
	return edits, nil
}
