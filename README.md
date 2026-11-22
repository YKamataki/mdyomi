# mdyomi（エムディーヨミ）

Markdown・XML・プレーンテキストに含まれる外国語の名詞を、日本語の読み上げに適したカタカナに変換する CLI です。たとえば `**Windows**でcomputerを使う。` は `**ウィンドウズ**でコンピューターを使う。` になります。読みの判定には OpenAI API を使います。入出力は UTF-8 です。

## 1. ビルドと実行

Go 1.22 以降でインストールできます。次のコマンドはソースと依存モジュールを取得し、Go のバイナリディレクトリ（`GOBIN`、既定では `$(go env GOPATH)/bin`）に `mdyomi` を配置します。このディレクトリが `PATH` に含まれていれば、以下の実行例の `./mdyomi` を `mdyomi` に置き換えて使えます。

```bash
go install github.com/YKamataki/mdyomi@latest
```

ソースからビルドする場合は、この README と同じディレクトリで次を実行します。カレントディレクトリに実行ファイルを作成します。

```bash
go build -trimpath -buildvcs=false -o mdyomi .
```

API キーは環境変数 `OPENAI_API_KEY` から読み込みます。Bash または zsh の対話端末では、次のコマンドでキーを画面に表示せず、現在のシェルに設定できます。すでに設定済みの場合は、変換コマンドから実行してください。

```bash
read -rs OPENAI_API_KEY
export OPENAI_API_KEY
```

変換例です。ファイルから読み込む場合も、パイプで渡す場合も結果は標準出力に出ます。

```bash
./mdyomi < article.md > article-yomi.md
printf '%s\n' '**Ubuntu** と `apt update`、USB。' | ./mdyomi
```

パイプの出力例は ``**ウブントゥ** と `apt update`、USB。`` です。読みはモデルと文脈によって変わります。

変換候補があると、その語と前後それぞれ最大32文字の本文、読み方のプロンプトが OpenAI に送信され、API の利用料金が発生します。複数の候補が近接していれば、送信する文脈には原稿の同じ部分が重複して含まれます。候補がない入力は通信せずにそのまま出力します。

## 2. 入力形式と原稿の保持

- 既定の `-format auto` では、先頭が `<` で単一ルート要素を持つ XML を XML として処理し、ほかの入力を Markdown として処理します。本文だけのテキストもそのまま渡せます。
- `-format markdown` は Markdown の構文を優先します。リンクや画像の表示名は変換対象です。リンク先・タイトル・参照定義・コードブロック・インラインコード・HTML タグ・先頭の YAML/TOML フロントマターは保持します。`[Ubuntu][]` のように表示名が参照キーも兼ねる場合、その表示名も保持します。
- `-format xml` は整形式の XML を要求します。要素内の本文を変換し、タグ、属性、名前空間、実体参照、コメント、処理命令、DOCTYPE、CDATA を保持します。`code`、`pre`、`script`、`style`、`kbd`、`samp`、`textarea` 要素の内容も保持します。
- 置換は原文上の候補語の位置にだけ適用します。改行コード、末尾改行の有無、空白、インデント、マークアップの表記は入力時のままです。通常の語と略語が連続する製品名では、大文字の略語部分が残ります。
- モデルは各候補を文脈から外国語の名詞・略称・コマンド名・その他に分類します。CLI は外国語の名詞と判定された候補だけに読みを適用します。小文字の略称や日本語と外国語で共通する漢字など、判断できない語は原綴りを残すよう指示しています。

## 3. 読み方の調整

既定のプロンプトは [prompt.txt](prompt.txt) です。編集して再ビルドすると、Go の `embed` により実行ファイルに組み込まれます。読みを調整しながら試す場合は、外部ファイルを直接指定できます。

```bash
./mdyomi -prompt ./prompt.txt -format xml < manuscript.xml > manuscript-yomi.xml
```

既定モデルは `gpt-5.4-mini` です。`-model` で Responses API と Structured Outputs に対応するモデルを指定できます。[モデル仕様](https://developers.openai.com/api/docs/models/gpt-5.4-mini)・[Structured Outputs](https://developers.openai.com/api/docs/guides/structured-outputs)

## 4. 終了状態と検証

成功時は変換した本文だけを標準出力へ書きます。入力解析・通信・応答検証に失敗した場合は診断を標準エラーに出し、非ゼロで終了します。入力全体をメモリに読み込み、全バッチが成功してから出力するため、後続バッチが失敗しても途中までの原稿は出力されません。

シェルの `>` はコマンド開始前に出力先を作成または切り詰めます。変換結果は入力と別のファイルに保存してください。

以下のテストは模擬 HTTP 応答で実行でき、API キーも API 利用料も不要です。

```bash
go test -buildvcs=false ./...
```

GNU Make と POSIX シェルがある環境では、次のコマンドで書式、モジュールのチェックサム、`go vet`、単体テスト、CommonMark の構文比較をまとめて検証できます。構文比較には依存先の goldmark に同梱された仕様例を使用します。初回は Go が依存モジュールを取得します。

```bash
make check
```

## 5. CI とリリース

`main` へのプルリクエストと push で GitHub Actions が `make check` を実行します。対象は Go 1.22 系と最新安定版です。最新安定版では Linux・macOS・Windows の各 amd64／arm64 向けに配布物をビルドし、Linux amd64 版を展開して標準入出力も検証します。API キーの設定は不要です。[Actions の実行履歴](https://github.com/YKamataki/mdyomi/actions/workflows/ci.yml)

各実行の `distribution` アーティファクトには、6種類のアーカイブと `SHA256SUMS` が入ります。Linux・macOS 向けは tar.gz、Windows 向けは zip です。各アーカイブに実行ファイル、README、ライセンスを同梱します。

メンテナーが `v1.2.3` のような `vメジャー.マイナー.パッチ` 形式のタグを GitHub に push すると、そのタグでも同じ検証を実行します。すべて成功すると、配布物とチェックサムを GitHub Release に添付して公開します。[リリース一覧](https://github.com/YKamataki/mdyomi/releases)

## 6. 不具合報告と変更提案

変換結果を報告するときは、再現できる最小の入力、期待する出力、実際の出力、使用したモデルと入力形式を添えてください。原稿に個人情報が含まれる場合は、架空の例でも再現することを確認してから共有してください。マークアップの扱いを変更する場合は、保持すべき記号や空白も含めてテストに追加してください。

## 7. ライセンス

mdyomi のソースコードと組み込みプロンプトは MIT ライセンスで配布します。Markdown パーサー goldmark の著作権表示とライセンス本文も同梱しています。[mdyomi のライセンス](LICENSE)・[goldmark のライセンス](LICENSES/goldmark.txt)
