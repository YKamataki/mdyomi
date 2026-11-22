package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Replace every eligible word, including words a model would normally keep.
// Links, code, inline HTML and the nesting of Markdown nodes must survive.
func TestCommonMarkStructure(t *testing.T) {
	path := os.Getenv("MDYOMI_COMMONMARK_SPEC")
	if path == "" {
		t.Skip("make check runs the CommonMark corpus from the goldmark module")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Markdown, Section string
		Example           int
	}
	if err := json.Unmarshal(data, &cases); err != nil || len(cases) == 0 {
		t.Fatalf("invalid or empty CommonMark corpus: %v", err)
	}
	replaceAll := func(_ context.Context, batch []candidate) ([]edit, error) {
		edits := make([]edit, 0, len(batch))
		for _, c := range batch {
			edits = append(edits, edit{ID: c.ID, Reading: "カナ"})
		}
		return edits, nil
	}
	for _, tc := range cases {
		src := []byte(tc.Markdown)
		prose, err := editableText(src, "markdown")
		if err != nil {
			t.Fatalf("example %d: %v", tc.Example, err)
		}
		out, err := convert(context.Background(), src, prose, replaceAll)
		if err != nil {
			t.Fatalf("example %d: %v", tc.Example, err)
		}
		if structure(out) != structure(src) {
			t.Errorf("example %d (%s): Markdown structure changed\ninput %q\noutput %q", tc.Example, tc.Section, src, out)
		}
	}
	t.Logf("checked %d CommonMark examples", len(cases))
}

func structure(src []byte) string {
	doc := markdown.Parser().Parse(text.NewReader(src))
	var b strings.Builder
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			b.WriteByte(')')
			return ast.WalkContinue, nil
		}
		fmt.Fprintf(&b, "(%s", n.Kind())
		switch x := n.(type) {
		case *ast.Link:
			fmt.Fprintf(&b, "%q,%q", x.Destination, x.Title)
		case *ast.Image:
			fmt.Fprintf(&b, "%q,%q", x.Destination, x.Title)
		case *ast.AutoLink:
			fmt.Fprintf(&b, "%q", x.URL(src))
		case *ast.CodeSpan:
			fmt.Fprintf(&b, "%q", x.Text(src))
		case *ast.RawHTML:
			fmt.Fprintf(&b, "%q", x.Segments.Value(src))
		case *ast.FencedCodeBlock:
			fmt.Fprintf(&b, "%q %q", x.Language(src), x.Lines().Value(src))
		case *ast.CodeBlock:
			fmt.Fprintf(&b, "%q", x.Lines().Value(src))
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}
