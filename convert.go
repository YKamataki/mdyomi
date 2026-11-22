package main

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"unicode"
	"unicode/utf8"
)

type span struct{ start, end int }

type candidate struct {
	ID     int    `json:"id"`
	Word   string `json:"word"`
	Before string `json:"before"`
	After  string `json:"after"`
}

type edit struct {
	ID      int    `json:"id"`
	Reading string `json:"reading"`
}

type lookup func(context.Context, []candidate) ([]edit, error)

var capitals = regexp.MustCompile(`\p{Lu}+`)
var kana = regexp.MustCompile(`^[ァ-ヺ][ァ-ヺー]*$`)

// Han is separate so a shared Japanese/Chinese spelling can be judged in
// context without consuming adjacent kana or Latin letters.
func letterClass(r rune) int {
	switch {
	case unicode.Is(unicode.Han, r):
		return 1
	case unicode.In(r, unicode.Hiragana, unicode.Katakana), r == 'ー', r == 'ｰ':
		return 0
	case unicode.IsLetter(r):
		return 2
	}
	return 0
}

func words(prose []byte) []span {
	var spans []span
	add := func(start, end int) {
		if start < end {
			spans = append(spans, span{start, end})
		}
	}
	split := func(start, end int) {
		word := prose[start:end]
		prev := start
		for _, m := range capitals.FindAllIndex(word, -1) {
			a, b := m[0], m[1]
			// In XMLParser the final capital starts the next word.
			next, _ := utf8.DecodeRune(word[b:])
			if unicode.IsLower(next) {
				_, size := utf8.DecodeLastRune(word[a:b])
				b -= size
			}
			if (a == 0 && b == len(word)) || utf8.RuneCount(word[a:b]) >= 2 {
				add(prev, start+a)
				prev = start + b
			}
		}
		add(prev, end)
	}
	start, class := 0, 0
	for i, r := range string(prose) {
		c := letterClass(r)
		if class != 0 && unicode.IsMark(r) {
			c = class
		}
		if c != class {
			if class != 0 {
				split(start, i)
			}
			start, class = i, c
		}
	}
	if class != 0 {
		split(start, len(prose))
	}
	return spans
}

// Keep context within the prose region and line. Walking from the candidate
// bounds the work even for a single, very long input line.
func makeCandidate(prose []byte, s span, id int) candidate {
	left, right := s.start, s.end
	for i := 0; i < 32 && left > 0; i++ {
		r, n := utf8.DecodeLastRune(prose[:left])
		if r == 0 || r == '\r' || r == '\n' {
			break
		}
		left -= n
	}
	for i := 0; i < 32 && right < len(prose); i++ {
		r, n := utf8.DecodeRune(prose[right:])
		if r == 0 || r == '\r' || r == '\n' {
			break
		}
		right += n
	}
	return candidate{id, string(prose[s.start:s.end]), string(prose[left:s.start]), string(prose[s.end:right])}
}

const batchSize = 48

func convert(ctx context.Context, src, prose []byte, readings lookup) ([]byte, error) {
	spans := words(prose)
	if len(spans) == 0 {
		return src, nil
	}
	var out bytes.Buffer
	last := 0
	for first := 0; first < len(spans); first += batchSize {
		group := spans[first:min(first+batchSize, len(spans))]
		batch := make([]candidate, len(group))
		for i, s := range group {
			batch[i] = makeCandidate(prose, s, i+1)
		}
		edits, err := readings(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("候補 %d–%d: %w", first+1, first+len(group), err)
		}
		replacements := make([]string, len(group))
		for _, e := range edits {
			if e.ID < 1 || e.ID > len(group) || !kana.MatchString(e.Reading) {
				return nil, fmt.Errorf("候補の範囲または読みが不正です (id=%d)", e.ID)
			}
			if replacements[e.ID-1] != "" {
				return nil, fmt.Errorf("候補 id=%d が重複しています", e.ID)
			}
			replacements[e.ID-1] = e.Reading
		}
		for i, reading := range replacements {
			if reading != "" {
				s := group[i]
				out.Write(src[last:s.start])
				out.WriteString(reading)
				last = s.end
			}
		}
	}
	out.Write(src[last:])
	return out.Bytes(), nil
}
