package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM, extension.Footnote),
	goldmark.WithParserOptions(parser.WithAttribute()))

var (
	// Applied to original bytes, before any entity decoding.
	entity  = regexp.MustCompile(`&(?:[\pL_:][\pL\pM\pN_.:-]*|#[0-9]+|#[xX][0-9A-Fa-f]+);`)
	address = regexp.MustCompile(`(?i)(?:[a-z][a-z0-9+.-]*://|mailto:|www\.)[^\x00\s<>"【】「」、。]+|[\w.+%-]+@[\w.-]+\.[a-z]{2,}`)
	// Capturing the name also lets us protect inline raw-code elements.
	// A tag-like opener without a closing '>' consumes the rest of the
	// contiguous region. CommonMark permits these in raw HTML blocks.
	tag = regexp.MustCompile(`(?is)<!--.*?(?:-->|\z)|<\?.*?(?:\?>|\z)|<!\[CDATA\[.*?(?:\]\]>|\z)|</?([\pL_:][\pL\pM\pN_.:-]*)(?:\s+(?:[^<>"']|"[^"]*"|'[^']*')*)?\s*/?>|</?[\pL_:][^\x00]*`)
)

// Both parsers produce a same-length view. Only visible text is copied into
// it. Original bytes, offsets and line endings are used for the final output.
func editableText(src []byte, format string) ([]byte, error) {
	start := bytes.TrimSpace(bytes.TrimPrefix(src, []byte("\uFEFF")))
	var prose []byte
	if format == "xml" || (format == "auto" && bytes.HasPrefix(start, []byte("<"))) {
		var err error
		prose, err = xmlText(src)
		declared := len(start) > 5 && bytes.HasPrefix(start, []byte("<?xml")) && bytes.IndexByte([]byte(" \t\r\n"), start[5]) >= 0
		if err != nil && (format == "xml" || declared) {
			return nil, fmt.Errorf("XML を解析できません: %w", err)
		}
	}
	if prose == nil {
		prose = markdownText(src)
	}
	for _, re := range []*regexp.Regexp{address, entity} {
		for _, match := range re.FindAllIndex(prose, -1) {
			clear(prose[match[0]:match[1]])
		}
	}
	return prose, nil
}

func xmlText(src []byte) ([]byte, error) {
	d := xml.NewDecoder(bytes.NewReader(src))
	// The original spelling of each entity is kept. External entities and
	// DTDs need no resolution, including when their definitions are external.
	d.Entity = map[string]string{}
	for _, ref := range entity.FindAll(src, -1) {
		if ref[1] != '#' {
			d.Entity[string(ref[1:len(ref)-1])] = string(ref)
		}
	}
	prose := make([]byte, len(src))
	depth, opaque, roots := 0, 0, 0
	for {
		start := int(d.InputOffset())
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		raw := src[start:int(d.InputOffset())]
		switch t := tok.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
			}
			depth++
			if opaque == 0 && codeElement(t.Name.Local) {
				opaque = depth
			}
		case xml.EndElement:
			if opaque == depth {
				opaque = 0
			}
			depth--
		case xml.CharData:
			if depth == 0 {
				if len(bytes.TrimSpace(bytes.TrimPrefix(raw, []byte("\uFEFF")))) > 0 {
					return nil, errors.New("ルート要素の外に本文があります")
				}
			} else if opaque == 0 && !bytes.HasPrefix(raw, []byte("<![CDATA[")) {
				copy(prose[start:], raw)
			}
		}
	}
	if roots != 1 {
		return nil, errors.New("ルート要素は一つ必要です")
	}
	return prose, nil
}

func codeElement(name string) bool {
	switch strings.ToLower(name) {
	case "code", "pre", "script", "style", "kbd", "samp", "textarea":
		return true
	}
	return false
}

// Skip a delimited YAML or TOML header before parsing the Markdown body.
func frontMatterEnd(src []byte) int {
	line, rest, ok := bytes.Cut(src, []byte("\n"))
	marker := string(bytes.TrimSuffix(line, []byte("\r")))
	if !ok || (marker != "---" && marker != "+++") {
		return 0
	}
	for len(rest) > 0 {
		line, rest, _ = bytes.Cut(rest, []byte("\n"))
		end := string(bytes.TrimSuffix(line, []byte("\r")))
		if end == marker || (marker == "---" && end == "...") {
			return len(src) - len(rest)
		}
	}
	return 0
}

func markdownText(src []byte) []byte {
	prose := make([]byte, len(src))
	bom := len(src) - len(bytes.TrimPrefix(src, []byte("\uFEFF")))
	offset := bom + frontMatterEnd(src[bom:])
	body, view := src[offset:], prose[offset:]
	doc := markdown.Parser().Parse(text.NewReader(body))
	var rawTags []text.Segment
	mark := func(s text.Segment) { copy(view[s.Start:s.Stop], body[s.Start:s.Stop]) }
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := node.(type) {
		case *ast.CodeSpan, *ast.CodeBlock, *ast.FencedCodeBlock, *ast.AutoLink:
			return ast.WalkSkipChildren, nil
		case *ast.Link:
			if n.Reference != nil && n.Reference.Type != ast.ReferenceLinkFull {
				return ast.WalkSkipChildren, nil
			}
		case *ast.Image:
			if n.Reference != nil && n.Reference.Type != ast.ReferenceLinkFull {
				return ast.WalkSkipChildren, nil
			}
		case *ast.Text:
			mark(n.Segment)
		case *ast.RawHTML:
			for i := range n.Segments.Len() {
				s := n.Segments.At(i)
				mark(s)
				rawTags = append(rawTags, s)
			}
		case *ast.HTMLBlock:
			// Types 1–5 contain raw code, comments or declarations.
			if n.HTMLBlockType >= ast.HTMLBlockType6 {
				for i := range n.Lines().Len() {
					mark(n.Lines().At(i))
				}
			}
		}
		return ast.WalkContinue, nil
	})
	// Inline HTML and namespaced XML may cross several Markdown text nodes.
	var opaqueName string
	opaqueStart := 0
	for _, m := range tag.FindAllSubmatchIndex(view, -1) {
		token := view[m[0]:m[1]]
		if m[2] >= 0 {
			name := strings.ToLower(string(view[m[2]:m[3]]))
			endTag := bytes.HasPrefix(token, []byte("</"))
			if opaqueName == name && endTag {
				clear(view[opaqueStart:m[1]])
				opaqueName = ""
			} else if opaqueName == "" && !endTag && codeElement(name) && !bytes.HasSuffix(token, []byte("/>")) {
				opaqueName, opaqueStart = name, m[0]
			}
		}
		clear(token)
	}
	if opaqueName != "" {
		clear(view[opaqueStart:])
	}
	for _, s := range rawTags {
		clear(view[s.Start:s.Stop])
	}
	return prose
}
