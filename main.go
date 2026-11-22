package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"time"
	"unicode/utf8"
)

//go:embed prompt.txt
var defaultPrompt string

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, client); err != nil {
		fmt.Fprintln(os.Stderr, "mdyomi:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, in io.Reader, out, diag io.Writer, client *http.Client) error {
	flags := flag.NewFlagSet("mdyomi", flag.ContinueOnError)
	flags.SetOutput(diag)
	model := flags.String("model", "gpt-5.4-mini", "OpenAI Responses API のモデル")
	format := flags.String("format", "auto", "入力形式 (auto, markdown, xml)")
	promptFile := flags.String("prompt", "", "組み込みプロンプトの代わりに使う UTF-8 ファイル")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("入力ファイルは標準入力から渡してください")
	}
	if *format != "auto" && *format != "markdown" && *format != "xml" {
		return errors.New("-format には auto, markdown, xml のいずれかを指定してください")
	}
	prompt := defaultPrompt
	if *promptFile != "" {
		b, err := os.ReadFile(*promptFile)
		if err != nil {
			return err
		}
		if !utf8.Valid(b) || len(b) == 0 {
			return errors.New("プロンプトには空でない UTF-8 テキストを指定してください")
		}
		prompt = string(b)
	}
	src, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	if !utf8.Valid(src) {
		return errors.New("入力を UTF-8 に変換してください")
	}
	prose, err := editableText(src, *format)
	if err != nil {
		return err
	}
	api := openAI{client: client, key: os.Getenv("OPENAI_API_KEY"), model: *model, prompt: prompt}
	result, err := convert(ctx, src, prose, api.readings)
	if err != nil {
		return err
	}
	_, err = out.Write(result)
	return err
}
