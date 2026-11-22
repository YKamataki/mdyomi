package main

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// Replace every eligible span, including words a good model would skip.
// Comparing the resulting bytes detects accidental exposure of syntax.
func TestEditableRanges(t *testing.T) {
	tests := []struct{ name, format, input, want string }{
		{"markdown", "auto",
			"# **Ubuntu**  \r\n[Java](docs/Java.md \"Java\") と ![Java][asset]\r\n\r\n[asset]: /Java.png\r\n",
			"# **カナ**  \r\n[カナ](docs/Java.md \"Java\") と ![カナ][asset]\r\n\r\n[asset]: /Java.png\r\n"},
		{"reference keys", "markdown",
			"[Ubuntu] [Ubuntu][] ![Ubuntu] [Ubuntu][key]\n\n[Ubuntu]: /url\n[key]: /url\n",
			"[Ubuntu] [Ubuntu][] ![Ubuntu] [カナ][key]\n\n[Ubuntu]: /url\n[key]: /url\n"},
		{"code", "auto",
			"`Java`、``a ` b``\n\n> ~~~sh\n> Ubuntu\n> ~~~\n\n    Java\n\nUbuntu",
			"`Java`、``a ` b``\n\n> ~~~sh\n> Ubuntu\n> ~~~\n\n    Java\n\nカナ"},
		{"inline tags", "markdown",
			"あなたとJava、ダウンロー<br/>ド <em title='a > b'>Java</em> &copy; <code>Java</code>",
			"あなたとカナ、ダウンロー<br/>ド <em title='a > b'>カナ</em> &copy; <code>Java</code>"},
		{"namespaces in prose", "markdown",
			"これは<x:item xml:lang='en'>Java</x:item>です",
			"これは<x:item xml:lang='en'>カナ</x:item>です"},
		{"raw html block", "markdown",
			"<div title='Java'>\nUbuntu <br> <code>Java</code>\n</div>\n",
			"<div title='Java'>\nカナ <br> <code>Java</code>\n</div>\n"},
		{"unfinished html block", "markdown",
			"<div title='Java'\n*Java*\n\nUbuntu\n",
			"<div title='Java'\n*Java*\n\nカナ\n"},
		{"references and metadata", "auto",
			"\uFEFF---\r\ntitle: Java\r\n---\r\n# Java {#ubuntu}\r\n\r\nJava[^java]\r\n\r\n[^java]: Java\r\n",
			"\uFEFF---\r\ntitle: Java\r\n---\r\n# カナ {#ubuntu}\r\n\r\nカナ[^java]\r\n\r\n[^java]: カナ\r\n"},
		{"addresses", "auto",
			"Ubuntu <https://host/Java> https://host/Java me@example.org &amp; &#x3b;",
			"カナ <https://host/Java> https://host/Java me@example.org &amp; &#x3b;"},
		{"xml", "auto",
			"<?xml version=\"1.0\"?>\r\n<!DOCTYPE doc [<!ENTITY name 'Java'>]>\r\n<x:doc xmlns:x='urn:example'>\n  <x:p a='1 > 0'>Java &name; &#x41;</x:p><!-- Java --><?q Java?><![CDATA[Java <]]><code>Echo</code></x:doc>",
			"<?xml version=\"1.0\"?>\r\n<!DOCTYPE doc [<!ENTITY name 'Java'>]>\r\n<x:doc xmlns:x='urn:example'>\n  <x:p a='1 > 0'>カナ &name; &#x41;</x:p><!-- Java --><?q Java?><![CDATA[Java <]]><code>Echo</code></x:doc>"},
		{"xml indentation", "xml", "<doc>\n    <p>Москва 서울</p>\n</doc>", "<doc>\n    <p>カナ カナ</p>\n</doc>"},
		{"xml URL", "xml", "<p>Java https://host/search?q=Java&amp;sort=Name</p>", "<p>カナ https://host/search?q=Java&amp;sort=Name</p>"},
		{"caps and numbers", "auto", "OS USB DVD OpenAIのChatGPT、XMLParser UTF-8 Java21", "OS USB DVD カナAIのカナGPT、XMLカナ UTF-8 カナ21"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := []byte(tc.input)
			prose, err := editableText(src, tc.format)
			if err != nil {
				t.Fatal(err)
			}
			got, err := convert(context.Background(), src, prose, func(_ context.Context, batch []candidate) ([]edit, error) {
				var edits []edit
				for _, c := range batch {
					edits = append(edits, edit{c.ID, "カナ"})
				}
				return edits, nil
			})
			if err != nil || string(got) != tc.want {
				t.Fatalf("err=%v\n got %q\nwant %q", err, got, tc.want)
			}
		})
	}
}

func TestLanguagesAndContext(t *testing.T) {
	src := []byte("についてrésuméとМосква、서울、深圳。**OpenAI**")
	p, err := editableText(src, "auto")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for i, s := range words(p) {
		c := makeCandidate(p, s, i+1)
		got = append(got, c.Word)
		if c.Word == "Open" && c.After != "AI" {
			t.Errorf("組み込み略語の文脈: %#v", c)
		}
	}
	if !slices.Equal(got, []string{"résumé", "Москва", "서울", "深圳", "Open"}) {
		t.Fatal(got)
	}
}

func TestRejectMalformedXML(t *testing.T) {
	for _, s := range []string{"<a><b></a>", "<a/><b/>", "stray<a/>"} {
		if _, err := editableText([]byte(s), "xml"); err == nil {
			t.Fatal(s)
		}
	}
}

func TestRejectInvalidEdits(t *testing.T) {
	src := []byte("Java OS")
	for _, edits := range [][]edit{
		{{0, "ジャバ"}}, {{2, "ジャバ"}}, {{1, ""}}, {{1, " ジャバ"}}, {{1, "<tag>"}},
		{{1, "ジャバ"}, {1, "ジャヴァ"}},
	} {
		_, err := convert(context.Background(), src, src, func(context.Context, []candidate) ([]edit, error) { return edits, nil })
		if err == nil {
			t.Fatal(edits)
		}
	}
}

func TestBatchBoundaries(t *testing.T) {
	src := []byte(strings.Repeat("Java ", batchSize*2+1))
	calls := 0
	got, err := convert(context.Background(), src, src, func(_ context.Context, batch []candidate) ([]edit, error) {
		calls++
		var result []edit
		for i := len(batch) - 1; i >= 0; i-- {
			result = append(result, edit{batch[i].ID, "ジャバ"})
		}
		return result, nil
	})
	if err != nil || calls != 3 || string(got) != strings.Repeat("ジャバ ", batchSize*2+1) {
		t.Fatalf("calls=%d err=%v got=%s", calls, err, got)
	}
}
