# NeoSwitch

[English](../README.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md) | **日本語** | [한국어](README.ko.md) | [Español](README.es.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Português (Brasil)](README.pt-BR.md) | [Русский](README.ru.md)

[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-D97757)](https://github.com/yldm-ai/neo-switch/blob/main/.claude-plugin/plugin.json) [![Release](https://img.shields.io/github/v/release/yldm-ai/neo-switch)](https://github.com/yldm-ai/neo-switch/releases/latest) [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/yldm-ai/neo-switch/blob/main/LICENSE)

NeoSwitch を使うと、Claude Code の設定・コネクタ・権限・ほかのウィンドウはそのままに、個別のセッションだけを Google の Gemini 4 Neon に切り替えられます。

[![3 ステップで NeoSwitch を設定する](../docs/media/neoswitch-demo.gif)](../docs/media/neoswitch-demo.mp4)

セッション単位で切り替えられます — 同じプロジェクトのほかのウィンドウは Claude のままです。プラグインは macOS と Linux で動作し、Windows にはまだ対応していません。

## ハイライト

- **セッション単位で切り替え、その場で戻せる。** `/neoswitch:on` と `/neoswitch:off` は現在のセッションの次のメッセージからだけ効きます。同じプロジェクトのほかのウィンドウや新しいセッションは Claude のままで、最後に切り替えたセッションを閉じるとプロジェクト設定も自動で取り除かれます。
- **Claude Code の機能がそのまま動く。** `ANTHROPIC_AUTH_TOKEN` を書き込まないため `claude.ai` コネクタがそのまま使え、Antigravity では組み込み `WebSearch` が Google 検索で動作します。Auto Mode もフェイルクローズの安全分類器でそのまま動き（再起動不要・Claude の利用枠消費なし）、`/effort` や任意の思考ブロック（`NEOSWITCH_SHOW_THINKING`）も Gemini に対応します。
- **モデル呼び出し不要の高速コマンドとクォータ表示。** Claude Code 2.1.326 以降では `/neoswitch:login`・`/neoswitch:on`・`/neoswitch:off`・`/neoswitch:status` がモデルターンを使わず即座に回答し、プロンプト下に実際の回答モデル行を表示します。`/neoswitch:status` はクォータを消費せずに 5 時間・週間の Antigravity 利用状況バーとリセット時刻を表示します（`-v` で全クォータグループと残クレジットも表示）。
- **長い実戦セッション向け設計。** ACP セッション再利用とチャンク分割で `agy` の 192 KB 切り捨てを回避し、スリムなカスタムエージェントで `agy` のプロンプト負荷を約 14k から 1k トークン未満に削減します。並列ツール呼び出しの JSON エスケープ修復や、プロンプト候補・セッションタイトルの `gemini-3.8-flash` への振り分けで Gemini 4 Neon のクォータを節約します。
- **ローカル完結・隔離・元に戻せる。** 通信は署名後に CA 秘密鍵を破棄するローカル TLS のみで、NeoSwitch は Google OAuth トークンを一切読み取り・保存・再生しません。`agy` は隔離ワークスペースで動き、独自のファイル・シェルツールはスリムエージェントと検証付き `PreToolUse` フックで二重に遮断されます。オフにするとご自身の `NODE_EXTRA_CA_CERTS` や `ANTHROPIC_CUSTOM_HEADERS` も元に戻ります。
- **SSH 対応と無停止アップデート。** `/neoswitch:login` はブラウザー認証と同時にリモート・SSH 向けのリンク＋コード方式にも対応します。プラグイン更新時は開いているウィンドウを閉じずに新しいデーモンへ自動でポートを引き継ぎ、ビルドやダウンロードに失敗してもキャッシュ版へフォールバックして `/neoswitch:off` を守ります。
- **複数のバックエンド。** 公式 `agy` 経由の Google アカウントのほか、`GEMINI_API_KEY`、Vertex AI、実験的な OpenAI Responses バックエンドに対応します。

## はじめかた

**1. プラグインをインストールする。** Claude Code で次の 2 行を 1 行ずつ実行します。各行は丸ごとコピーしてください。1 行目は `neo-switch`、2 行目は `neoswitch` です。

```text
/plugin marketplace add yldm-ai/neo-switch
```

```text
/plugin install neoswitch@neoswitch
```

Google や Claude Code の変更に追従する修正を自動で受け取れるよう、自動更新をオンにしておきます。`/plugin` を実行し、**Marketplaces** タブで `neoswitch` を選んで auto-update をオンにしてください。サードパーティのマーケットプレイスでは Claude Code は自動更新を既定でオフにしています。

**2. Google アカウントでログインする。** 切り替えたいプロジェクトで Claude Code を開き、次を実行します。

```text
/neoswitch:login
```

Google の利用規約警告を表示し、Google 公式の [Antigravity CLI（`agy`）](https://antigravity.google/download) が未導入なら `~/.local/bin` にインストール（約 230 MB）したうえでブラウザーで Google サインインを開き、このセッションを切り替えます。リモート・SSH 向けにリンクも表示されるため、任意の端末でサインインして Claude Code 2.1.326 以降のダイアログにコードを貼り付けられます（古い版や WSL、SSH 経由のデスクトップアプリでは `/neoswitch:login <code>` を実行）。アカウント切り替えはもう一度 `/neoswitch:login`、維持は `/neoswitch:login cancel` です。API キーや Vertex AI を使う場合は [docs/cli.md](../docs/cli.md#gemini-api-key)（英語）を参照してください。

> [!WARNING]
> [Google Antigravity 利用規約](https://antigravity.google/terms)（第 6 条）は、ほかのツールを通じて Antigravity を使うことを禁じており、NeoSwitch はそうしたツールにあたります。Google はそうした利用者の Antigravity のアカウントを停止または解約することがあります。`/neoswitch:login` は毎回この警告を表示します。このリスクを受け入れられない場合は、ログインしないでください。

**3. 次のメッセージを送る。** このセッションは次のメッセージから Gemini 4 Neon を使い、ほかのウィンドウや新しいセッションは Claude のままです。ゲートウェイ経由で起動したため再起動を求められた場合は、ゲートウェイを使わずに同じプロジェクトで `claude` を起動し直してください。

| コマンド | 内容 |
|---|---|
| `/neoswitch:login [code \| cancel]` | 公式 `agy` でのサインイン、Google アカウント切り替え、または SSH コード認証を完了し、このセッションをオンにする |
| `/neoswitch:on` | このセッションを次のメッセージから Gemini 4 Neon に切り替える |
| `/neoswitch:off` | このセッションを次のメッセージから Claude に戻す |
| `/neoswitch:status [-v]` | 現在のモデル、Google サインイン状態、5 時間・週間クォータバーを表示（`-v` で全クォータグループ、クレジット、バックエンド、デーモン詳細を追加） |

## ほかのツールとの違い

| | **NeoSwitch** | **設定管理・ルーター**<br>（例: CC Switch、Claude Code Router） | **独立した Antigravity プロキシ**<br>（例: antigravity-claude-proxy） |
|---|---|---|---|
| **構成** | Claude Code ネイティブプラグイン（デスクトップアプリや常駐サーバー管理は不要） | 複数の CLI 設定をまとめて管理する別個のデスクトップアプリ | 独立した Node.js プロキシサーバー + Web UI |
| **切り替え範囲** | **セッション単位**（`/neoswitch:on`・`/neoswitch:off`）。ほかのウィンドウは Claude のまま | プロバイダーのグローバル切り替え、または共通ローカルルーター | グローバルな `~/.claude/settings.json` やシェル環境変数 |
| **Claude Code の機能** | **そのまま維持** — `ANTHROPIC_AUTH_TOKEN` を設定しないため、`claude.ai` コネクタ、`WebSearch`、Auto Mode、クォータ表示、ネイティブ mod コマンドがそのまま動作 | 外部設定またはルーター経由で管理 | `ANTHROPIC_AUTH_TOKEN` を設定するため `claude.ai` コネクタが無効化される |
| **Google サインイン** | 公式 `agy` 経由で Google アカウントを使用。**OAuth トークンの読み取り・保存・再生は一切せず**、`agy` 自身のツールも遮断 | 主に API キーやサードパーティ中継を利用（`agy` 統合なし） | ローカルの OAuth トークンを保存・読み取りし、非公開エンドポイントを直接呼び出す |
| **向いている用途** | 普段の Claude Code 環境を保ったまま、必要なセッションだけ Gemini 4 Neon に切り替えたい | 複数の CLI・API キー・中継先・MCP を一か所で管理したい | 複数アカウントのローテーション、Windows 対応、Antigravity 経由での Claude モデル利用 |

## 知っておくとよいこと

- **モデル表示:** Claude Code 上部のヘッダーには `claude-opus-5-5` のような Claude モデル名が表示されたままで、`/model` でもバックエンドモデルは変わりません。Claude Code 2.1.326 以降では、NeoSwitch が有効なあいだプロンプト下に `Gemini 4 Neon answers this session` と表示されます。
- **応答待ち・`/effort`・思考ブロック:** 最初の文字が出るまで 5〜40 秒かかることがあります。`/effort low` で応答開始を早められ（Antigravity と Gemini API / Vertex AI の両方で有効）、`NEOSWITCH_SHOW_THINKING=on` を設定すると上流の思考を Claude Code の思考ブロックとして表示します。
- **インストール・更新後の初回実行:** インストールや更新後最初の NeoSwitch コマンドは、検証済みリリースバイナリのビルドまたはダウンロードのため最大 1 分ほどかかることがあります。
- **Auto Mode:** NeoSwitch 内蔵のフェイルクローズ安全分類器（Antigravity では `gemini-3.8-flash`、Claude 利用枠消費なし・再起動不要）でそのまま動きます。無効化は `NEOSWITCH_SAFEGUARD=off`、ご自身の Claude ログインで判定させたい場合は `CLAUDE_CODE_AUTO_MODE_SERVER=0 claude` で起動してください（Claude 利用枠を少し消費し、無応答時は Gemini にフォールバックします）。
- **1 日 1 回の案内:** NeoSwitch の設定後は、使っていない新しいセッションの冒頭にプロジェクトあたり 1 日 1 回まで `/neoswitch:on` の案内が出ます（`/neoswitch:off` を実行したプロジェクトでは非表示。`NEOSWITCH_NO_HINT=1` でオフ）。
- **自動更新:** 新しいリリースが出るとセッション通知と `/neoswitch:status` でお知らせします。自動更新がオンなら、新しいセッションが始まったタイミングで開いているウィンドウも自動で新しいデーモンへ移行します。

## うまくいかないとき

<details>
<summary>よくある問題と対処法（クリックして展開）</summary>

| 表示される内容 | 対処 |
|---|---|
| セッション開始時のメッセージに、NeoSwitch がまだ設定されていないと出る | `/neoswitch:login` を実行します。次のメッセージから Gemini 4 Neon を使います。 |
| `agy` がインストールされていないと表示される | `/neoswitch:login` が自動ではインストールできませんでした。ターミナルで `curl -fsSL https://antigravity.google/cli/install.sh \| bash` を実行するか、[Google のダウンロードページ](https://antigravity.google/download)からインストールしてから、もう一度 `/neoswitch:login` を実行します。 |
| どのメッセージも接続エラーになる、またはセッション開始時のメッセージにこのセッションは回答できないと出る | `/neoswitch:off` を実行します。Claude Code 2.1.326 以降ではモデルを呼ばずにローカルで Claude に戻します。デーモン自体が動いていない、または失敗が続く場合は、`/exit` で終了して `claude` を起動し直してください。新しいセッションは Claude を使い、`/neoswitch:status` で原因を確認できます。 |
| ほかの Claude Code ウィンドウが以前の設定を共有していると出る | このセッションで `/neoswitch:on` を実行すると、開いているウィンドウが自動で現行バージョンへ移行します。 |
| 別の認証ソースが設定されているため `claude.ai` コネクタが無効と警告が出る | 現在の NeoSwitch はこの警告の原因になりません。以前のバージョンが原因でした。プラグインを更新してください（下記参照）。それでも出る場合は、別のツールが Claude Code に `ANTHROPIC_AUTH_TOKEN` または `ANTHROPIC_API_KEY` を設定しています。 |
| NeoSwitch が使っていたポートを別のプログラムが占有したと出る | NeoSwitch はすでに空きポートへ移り、このプロジェクトの次のセッションを Claude に戻しています。新しいセッションを開き（Claude を使用）、Gemini を使うには `/neoswitch:on` を実行してください。 |
| NeoSwitch がプログラムをダウンロードできなかったと出る | インターネット接続を確認し、Claude Code を起動し直します。このコンピューターで以前 NeoSwitch が動いていた場合は、ダウンロードできるまでキャッシュ済みの旧バージョンを使い続けます。 |
| `/neoswitch:off` 自体が NeoSwitch を実行できなかったと出る | プロジェクトの `.claude/settings.local.json` から削除する項目が表示されます。それを削除して新しいセッションを始めます。[NeoSwitch の削除](#オフ更新削除)の手順 1 にも同じ項目があります。 |
| NeoSwitch を削除した後、プロジェクトで Claude Code が接続できない | そのプロジェクトはまだ NeoSwitch を向いています。[NeoSwitch の削除](#オフ更新削除)の手順 1 のとおり `.claude/settings.local.json` からその項目を削除し、新しいセッションを始めます。 |
| `/neoswitch:login` が不明なコマンドになる | プラグインを更新し（下記参照）、Claude Code を再起動してから `/reload-plugins` を実行します。 |
| Google アカウントの Antigravity プランに `gemini-4-neon` がないと出る | NeoSwitch はこのセッションを Claude のままにし、プランで利用できるモデル一覧を表示しています。`gemini-4-neon` を含むアカウントで `/neoswitch:login` し直すか、一覧のモデルを `GEMINI_MODEL` に設定してください。 |

</details>

## オフ・更新・削除

- `/neoswitch:off` で、このセッションを次のメッセージからご自身の Claude のログインで Claude に戻します。`/neoswitch:on` で、設定し直さずにまた Gemini 4 Neon に切り替えます。セッションを閉じると切り替えも終わり、新しいセッションはすべて Claude で始まります。

手動で更新するには、ターミナルで次を実行してから新しい Claude Code セッションを開きます（すでに開いているウィンドウも自動で新しい版に移行します）。

```bash
claude plugin marketplace update neoswitch
claude plugin update neoswitch@neoswitch
```

マーケットプレイスを旧名 `yldm-ai/NeoSwitch` / `yldm-ai/neon-switch` で追加した場合は、先に `claude plugin marketplace add yldm-ai/neo-switch` で追加し直してください。

NeoSwitch を削除するには:

1. 切り替えた各プロジェクトで、先に `/neoswitch:off` を実行します。オンのまま残したプロジェクトは NeoSwitch を向いたままになり、NeoSwitch を削除すると回答できなくなります。プラグインをすでに削除した場合は、該当する各プロジェクトの `.claude/settings.local.json` を開き、`env` オブジェクトを次のように直してください: `https://127.0.0.1:` で始まる `ANTHROPIC_BASE_URL` を削除します（以前のバージョンでは `http://127.0.0.1:` で、隣の `ANTHROPIC_AUTH_TOKEN` も一緒に削除します）。`ANTHROPIC_CUSTOM_HEADERS` の `X-NeoSwitch-Token` の行を削除し、ほかに何も残らなければ項目ごと削除します。NeoSwitch のフォルダーの `tls/bundle.pem` を指す `NODE_EXTRA_CA_CERTS` を削除し、自分の CA ファイルを設定していた場合はそのパスに戻します。自分で設定したのでなければ、値が `1` の `CLAUDE_CODE_GATEWAY_HINT_HEADERS` と値が `0` の `CLAUDE_CODE_USE_GATEWAY` も削除します。
2. NeoSwitch を使っている Claude Code ウィンドウを閉じ、ターミナルで次を実行します。

```bash
claude plugin uninstall neoswitch@neoswitch
claude plugin marketplace remove neoswitch
```

3. NeoSwitch がプロジェクトの外に保存しているファイルを削除します。

| 内容 | 場所 |
|---|---|
| プラグインがダウンロードまたはビルドした NeoSwitch のプログラム | `$XDG_CACHE_HOME/neoswitch/`。`XDG_CACHE_HOME` が未設定なら `~/.cache/neoswitch/` |
| 状態ファイル、TLS バンドル、隔離 `agy` ワークスペース、`daemon.log` | `NEOSWITCH_STATE_DIR` が設定されていればそこ。なければ macOS は `~/Library/Application Support/neoswitch/`、Linux は `$XDG_CONFIG_HOME/neoswitch/` または `~/.config/neoswitch/` |

Google のログイン情報は NeoSwitch ではなく `agy` が保存しており、そのまま残ります。`/neoswitch:login` がインストールした場合も含め、`agy` 本体も残ります。

## NeoSwitch が変更するもの

あるプロジェクトのセッションが Gemini 4 Neon を使っている間、NeoSwitch はそのプロジェクトの `.claude/settings.local.json` の `env` オブジェクトに 4 つの項目を追加し、そこで使うセッションがなくなると取り除きます。

- `ANTHROPIC_BASE_URL`、`ANTHROPIC_CUSTOM_HEADERS`、`NODE_EXTRA_CA_CERTS`: そのプロジェクトの Claude Code のリクエストを、NeoSwitch だけが応答できる暗号化された接続で、このコンピューター上の NeoSwitch に送ります。切り替えたセッションには Gemini 4 Neon が答え、それ以外のリクエストはご自身の Claude のログインのまま Claude にそのまま渡します。NeoSwitch はそのログインを保存も記録もしません。ヘッダーのキーはこのコンピューターの NeoSwitch にしか使えません。ファイルに自分で設定した `NODE_EXTRA_CA_CERTS` や `ANTHROPIC_CUSTOM_HEADERS` があった場合、NeoSwitch はその写しを残し、自分の項目を削除するときに元の値に戻します。
- `CLAUDE_CODE_GATEWAY_HINT_HEADERS=1`: リクエストが会話本体・サブエージェント・圧縮・プロンプト候補などの背景処理のどれから来たかを Claude Code にヘッダーで伝えさせます。NeoSwitch が会話専用の Gemini セッションを維持するために使います。ご自身でこの項目を設定している場合はそのままにします。

Git プロジェクトでは、NeoSwitch は `.claude/settings.local.json` をそのクローン専用の除外リスト（`.git/info/exclude`）にも追加し、トークンがコミットに入らないようにします。`.gitignore` は変更しません。

NeoSwitch はこのコンピューターの中だけで動き、このコンピューターからの接続しか受け付けません。プロンプト、回答、キー、ログイン情報は記録せず、私たちのサーバーも経由しません。Google のログイン情報は `agy` の中にあり、NeoSwitch が読むことはありません。

## さらに詳しく

使える機能と使えない機能、NeoSwitch のしくみ、単体のコマンドラインツール、環境変数、ロードマップ、開発については [docs/cli.md](../docs/cli.md)（英語）を参照してください。[Antigravity ログインの詳細](../docs/antigravity.md)と[プロトコル仕様](../docs/protocol-contract.md)もあります。

貢献の方法は [CONTRIBUTING.md](../CONTRIBUTING.md)、脆弱性の非公開報告は [SECURITY.md](../SECURITY.md)、行動規範は [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md) を参照してください。

MIT License
