package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func mockDecisions(batch []candidate, dictionary map[string]string) *http.Response {
	var decisions []map[string]any
	for _, c := range batch {
		category := "other"
		reading := dictionary[c.Word]
		if c.Word == "apt" {
			// A stray reading for a protected category must never reach stdout.
			category, reading = "command_identifier", "アプト"
		} else if reading != "" {
			category = "foreign_noun"
		}
		decisions = append(decisions, map[string]any{"id": c.ID, "category": category, "reading": reading})
	}
	data, _ := json.Marshal(map[string]any{"decisions": decisions})
	answer, _ := json.Marshal(map[string]any{"status": "completed", "output": []any{
		map[string]any{"type": "reasoning"}, map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": string(data)}}},
	}})
	return response(200, string(answer))
}

func TestFilter(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "mock")
	input := "Windowsでcomputerの世界が広がります。\r\nOpenAIのChatGPTを今すぐダウンロード\r\naptを使用。OS、USB、DVD。あなたとJava、今すぐダウンロー<br/>ド"
	want := "ウィンドウズでコンピューターの世界が広がります。\r\nオープンAIのチャットGPTを今すぐダウンロード\r\naptを使用。OS、USB、DVD。あなたとジャバ、今すぐダウンロー<br/>ド"
	dictionary := map[string]string{"Windows": "ウィンドウズ", "computer": "コンピューター", "Open": "オープン", "Chat": "チャット", "Java": "ジャバ"}
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != endpoint || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer mock" {
			t.Fatal("unexpected request")
		}
		var req struct {
			Input, Model, Instructions string
			Store                      *bool
			Text                       struct {
				Format struct {
					Type   string
					Strict bool
				}
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Model != "test-model" || req.Instructions != defaultPrompt || req.Store == nil || *req.Store || req.Text.Format.Type != "json_schema" || !req.Text.Format.Strict {
			t.Fatal("request settings")
		}
		var batch []candidate
		if err := json.Unmarshal([]byte(req.Input), &batch); err != nil {
			t.Fatal(err)
		}
		return mockDecisions(batch, dictionary), nil
	})}
	var out, diag bytes.Buffer
	err := run(context.Background(), []string{"-model", "test-model"}, strings.NewReader(input), &out, &diag, client)
	if err != nil || out.String() != want || diag.Len() != 0 {
		t.Fatalf("err=%v diag=%s\n got %q\nwant %q", err, &diag, out.String(), want)
	}
}

func TestPromptOverride(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "mock")
	path := filepath.Join(t.TempDir(), "custom.txt")
	if err := os.WriteFile(path, []byte("独自の読みの指示"), 0600); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		var req struct{ Instructions string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Instructions != "独自の読みの指示" {
			t.Fatalf("instructions=%q err=%v", req.Instructions, err)
		}
		return mockDecisions([]candidate{{ID: 1, Word: "Ubuntu"}}, nil), nil
	})}
	var out bytes.Buffer
	if err := run(context.Background(), []string{"-prompt", path}, strings.NewReader("Ubuntu"), &out, io.Discard, client); err != nil || out.String() != "Ubuntu" {
		t.Fatalf("out=%q err=%v", out.String(), err)
	}
}

func TestNoPartialOutput(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "mock")
	n := 0
	client := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		n++
		if n == 1 {
			batch := make([]candidate, batchSize)
			for i := range batch {
				batch[i] = candidate{ID: i + 1, Word: "Java"}
			}
			return mockDecisions(batch, map[string]string{"Java": "ジャバ"}), nil
		}
		return response(429, ""), nil
	})}
	var out bytes.Buffer
	err := run(context.Background(), nil, strings.NewReader(strings.Repeat("Java ", batchSize+1)), &out, io.Discard, client)
	if err == nil || !strings.Contains(err.Error(), "429") || out.Len() != 0 || n != 2 {
		t.Fatalf("err=%v bytes=%d calls=%d", err, out.Len(), n)
	}
}

func TestNoNetwork(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	client := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected network request"); return nil, nil })}
	for _, input := range []string{"", "ひらがなとカタカナ\r\n", "OS USB DVD\n`Ubuntu`", "<tag a='foo'/>"} {
		var out bytes.Buffer
		err := run(context.Background(), nil, strings.NewReader(input), &out, io.Discard, client)
		if err != nil || out.String() != input {
			t.Fatalf("in=%q err=%v out=%q", input, err, out.String())
		}
	}
	for _, input := range []string{"Ubuntu", "\xff"} {
		var out bytes.Buffer
		if err := run(context.Background(), nil, strings.NewReader(input), &out, io.Discard, client); err == nil || out.Len() != 0 {
			t.Fatalf("in=%q err=%v", input, err)
		}
	}
}

func TestResponseErrors(t *testing.T) {
	for _, body := range []string{
		`{"status":"incomplete","output":[]}`,
		`{"status":"completed","output":[{"type":"message","content":[{"type":"refusal"}]}]}`,
		`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"{}"}]}]}`,
		`{"status":"completed","output":[]}`,
	} {
		if _, err := decodeReadings([]byte(body), 1); err == nil {
			t.Fatal(body)
		}
	}
}
