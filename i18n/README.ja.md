# NeoSwitch

[English](../README.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md) | **日本語** | [한국어](README.ko.md) | [Español](README.es.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Português (Brasil)](README.pt-BR.md) | [Русский](README.ru.md)

[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-D97757)](https://github.com/yldm-ai/neo-switch/blob/main/.claude-plugin/plugin.json) [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/yldm-ai/neo-switch/blob/main/LICENSE)

NeoSwitch を使うと、Claude Code はそのままに、Claude の代わりに Google の Gemini 4 Neon モデルが回答します。

[![3 ステップで NeoSwitch を設定する](../docs/media/neoswitch-demo.gif)](../docs/media/neoswitch-demo.mp4)

切り替えはプロジェクトごとで、ほかのプロジェクトは Claude のまま使えます。プラグインは macOS と Linux で動作します。Windows にはまだ対応していません。

## はじめかた

**1. プラグインをインストールする。** Claude Code で次の 2 行を 1 行ずつ実行します。各行は丸ごとコピーしてください。1 行目は `neo-switch`、2 行目は `neoswitch` です。

```text
/plugin marketplace add yldm-ai/neo-switch
```

```text
/plugin install neoswitch@neoswitch
```

続けて自動更新をオンにしておくと、NeoSwitch が依存する部分を Google や Claude Code が変更したときも修正が自動で届きます。`/plugin` を実行し、**Marketplaces** タブで `neoswitch` を選んで auto-update をオンにします。Anthropic 以外のマーケットプレイスでは、Claude Code は自動更新をオフにしています。

**2. Google アカウントでログインする。** 切り替えたいプロジェクトで Claude Code を開き、次を実行します。

```text
/neoswitch:login
```

Google の利用規約に関する警告を表示したあと、Google の [Antigravity コマンドラインツール（`agy`）](https://antigravity.google/download)を通じてブラウザで Google のログインを開き、最後にこのプロジェクトを切り替えます。このコンピューターに `agy` がまだなければ、先に Google 公式のインストーラーで `~/.local/bin` にインストールします（約 230 MB。インストーラーはこのフォルダーをシェルの PATH にも追加します）。ターミナルに慣れている方は Gemini API キーも使えます。[docs/cli.md](../docs/cli.md#gemini-api-key)（英語）を参照してください。

開いたブラウザーを使えない場合（SSH で接続した先のコンピューターなど）に備えて、コマンドはリンクも表示します。任意のデバイスのブラウザーでそのリンクを開いてサインインし、ページに表示されたコードを付けて `/neoswitch:login` を実行してください。サインイン済みのまま `/neoswitch:login` をもう一度実行すると、別の Google アカウントに切り替えられます。完了するまでは今のアカウントのままで、`/neoswitch:login cancel` を実行するとそのまま維持されます。

> [!WARNING]
> [Google Antigravity 利用規約](https://antigravity.google/terms)（第 6 条）は、ほかのツールを通じて Antigravity を使うことを禁じており、NeoSwitch はそうしたツールにあたります。Google はそうした利用者の Antigravity のアカウントを停止または解約することがあります。`/neoswitch:login` は毎回この警告を表示します。このリスクを受け入れられない場合は、ログインしないでください。

**3. 次のメッセージを送る。** コマンドを実行したウィンドウは次のメッセージから Gemini 4 Neon を使います。ほかのウィンドウや新しいセッションは、ご自身の Claude のログインと connectors のまま Claude を使い続けます。Gemini 4 Neon で答えさせたいセッションごとに `/neoswitch:on` を実行してください。コマンドの返信で再起動を求められた場合（Claude Code をゲートウェイ経由で起動した場合）は、ゲートウェイを使わずに同じプロジェクトでもう一度 `claude` を起動してください。

## 切り替え後に表示されるもの

- NeoSwitch の設定が済むと、NeoSwitch を使っていない新しいセッションの冒頭に、プロジェクトごとに 1 日 1 回まで次のメッセージが出ます: `NeoSwitch is set up: /neoswitch:on answers this session with Gemini 4 Neon, and /neoswitch:off returns it to Claude any time.` `/neoswitch:off` を実行したプロジェクトでは出ません。環境変数に `NEOSWITCH_NO_HINT=1` を設定すると止まります。
- ヘッダーには `claude-opus-5-5` のような Claude のモデル名が表示されたままですが、ただの表示です。回答しているのは Gemini 4 Neon です。`/model` でモデルを選んでも変わりません。
- 回答の最初の文字が出るまで 5〜40 秒かかることがありますが、正常です。`/neoswitch:login` でログインした場合は、`/effort low` で回答が早く始まります。
- インストールや更新のあと最初の NeoSwitch コマンドは、準備のために最大 1 分ほどかかることがあります。
- `/neoswitch:status` を実行すると、このセッションが Claude と Gemini 4 Neon のどちらを使っているか、次に何をすればよいかがいつでもわかります。
- 新しい NeoSwitch が出ると、セッション開始時のメッセージでお知らせし、更新コマンドを表示します。自動更新がオンなら Claude Code が自分でインストールし、Claude Code のウィンドウをすべて閉じて開き直すと反映されます。

## うまくいかないとき

| 表示される内容 | 対処 |
|---|---|
| セッション開始時のメッセージに、NeoSwitch がまだ設定されていないと出る | `/neoswitch:login` を実行します。次のメッセージから Gemini 4 Neon を使います。 |
| `agy` がインストールされていないと表示される | `/neoswitch:login` が自動ではインストールできませんでした。ターミナルで `curl -fsSL https://antigravity.google/cli/install.sh \| bash` を実行するか、[Google のダウンロードページ](https://antigravity.google/download)からインストールしてから、もう一度 `/neoswitch:login` を実行します。 |
| どのメッセージも接続エラーになる、またはセッション開始時のメッセージにこのセッションは回答できないと出る | `/neoswitch:off` を実行します。そのコマンド自体の返答が失敗しても、このセッションは次のメッセージから Claude に戻ります。それでもメッセージが失敗する場合は、`/exit` と入力して `claude` を起動し直してください。新しいセッションは Claude を使い、`/neoswitch:status` で何を直せばよいかがわかります。 |
| ほかの Claude Code ウィンドウが以前の設定を共有していると出る | Claude Code のウィンドウをすべて閉じてもう一度開き、`/neoswitch:on` を実行します。 |
| 別の認証ソースが設定されているため claude.ai connectors が無効と警告が出る | 現在の NeoSwitch はこの警告の原因になりません。以前のバージョンが原因でした。プラグインを更新してください（下記参照）。それでも出る場合は、別のツールが Claude Code に `ANTHROPIC_AUTH_TOKEN` または `ANTHROPIC_API_KEY` を設定しています。 |
| NeoSwitch が必要とするポートを別のプログラムが使っていると出る | そのプログラムを閉じて Claude Code を起動し直すか、`/neoswitch:off` を実行します。 |
| NeoSwitch がプログラムをダウンロードできなかったと出る | インターネット接続を確認し、Claude Code を起動し直します。 |
| `/neoswitch:login` が不明なコマンドになる | プラグインを更新し（下記参照）、Claude Code を再起動してから `/reload-plugins` を実行します。 |
| Google アカウントの Antigravity プランに gemini-4-neon がないと出る | プランに Gemini 4 Neon が含まれていないため、NeoSwitch はそれで回答できず、プロジェクトは Claude のままです。`/neoswitch:login` をもう一度実行して、プランに含まれるアカウントでサインインするか、`/neoswitch:off` を実行して新しいセッションを始め、Claude に戻ってください。 |

## オフ・更新・削除

- `/neoswitch:off` で、このセッションを次のメッセージからご自身の Claude のログインで Claude に戻します。`/neoswitch:on` で、設定し直さずにまた Gemini 4 Neon に切り替えます。セッションを閉じると切り替えも終わり、新しいセッションはすべて Claude で始まります。

手動で更新するには、ターミナルで次を実行してから、Claude Code のウィンドウをすべて閉じて開き直します。

```bash
claude plugin marketplace update neoswitch
claude plugin update neoswitch@neoswitch
```

マーケットプレイスを旧名 `yldm-ai/NeoSwitch` / `yldm-ai/neon-switch` で追加した場合は、先に `claude plugin marketplace add yldm-ai/neo-switch` で追加し直してください。

NeoSwitch を削除するには:

1. 切り替えた各プロジェクトで、先に `/neoswitch:off` を実行します。オンのまま残したプロジェクトは NeoSwitch を向いたままになり、NeoSwitch を削除すると回答できなくなります。プラグインをすでに削除した場合は、該当する各プロジェクトの `.claude/settings.local.json` を開き、`env` オブジェクトから `ANTHROPIC_BASE_URL`、`ANTHROPIC_CUSTOM_HEADERS`、`NODE_EXTRA_CA_CERTS`、`CLAUDE_CODE_USE_GATEWAY`、以前のバージョンの `ANTHROPIC_AUTH_TOKEN` を削除してください。
2. NeoSwitch を使っている Claude Code ウィンドウを閉じ、ターミナルで次を実行します。

```bash
claude plugin uninstall neoswitch@neoswitch
claude plugin marketplace remove neoswitch
```

3. NeoSwitch がプロジェクトの外に保存しているファイルを削除します。

| 内容 | 場所 |
|---|---|
| プラグインがダウンロードまたはビルドした NeoSwitch のプログラム | `$XDG_CACHE_HOME/neoswitch/`。`XDG_CACHE_HOME` が未設定なら `~/.cache/neoswitch/` |
| 設定とログ: `proxy.json`（アドレス、アクセスキー、選んだ方法）、`daemon.log` | `NEOSWITCH_STATE_DIR` が設定されていればそこ。なければ macOS は `~/Library/Application Support/neoswitch/`、Linux は `$XDG_CONFIG_HOME/neoswitch/` または `~/.config/neoswitch/` |

Google のログイン情報は NeoSwitch ではなく `agy` が保存しており、そのまま残ります。`/neoswitch:login` がインストールした場合も含め、`agy` 本体も残ります。

## NeoSwitch が変更するもの

あるプロジェクトのセッションが Gemini 4 Neon を使っている間、NeoSwitch はそのプロジェクトの `.claude/settings.local.json` の `env` オブジェクトに 4 つの項目を追加し、そこで使うセッションがなくなると取り除きます。

- `ANTHROPIC_BASE_URL`、`ANTHROPIC_CUSTOM_HEADERS`、`NODE_EXTRA_CA_CERTS`: そのプロジェクトの Claude Code のリクエストを、NeoSwitch だけが応答できる暗号化された接続で、このコンピューター上の NeoSwitch に送ります。切り替えたセッションには Gemini 4 Neon が答え、それ以外のリクエストはご自身の Claude のログインのまま Claude にそのまま渡します。NeoSwitch はそのログインを保存も記録もしません。ヘッダーのキーはこのコンピューターの NeoSwitch にしか使えません。
- `CLAUDE_CODE_USE_GATEWAY=0`: それらのセッションを NeoSwitch に直接つなぎます。別のゲートウェイ経由で Claude Code を起動するツールの中にはゲートウェイモードをオンにするものがあり、そのままだと Claude Code は NeoSwitch を無視してしまいます。

Git プロジェクトでは、NeoSwitch は `.claude/settings.local.json` をそのクローン専用の除外リスト（`.git/info/exclude`）にも追加し、トークンがコミットに入らないようにします。`.gitignore` は変更しません。

NeoSwitch はこのコンピューターの中だけで動き、このコンピューターからの接続しか受け付けません。プロンプト、回答、キー、ログイン情報は記録せず、私たちのサーバーも経由しません。Google のログイン情報は `agy` の中にあり、NeoSwitch が読むことはありません。

## さらに詳しく

使える機能と使えない機能、NeoSwitch のしくみ、単体のコマンドラインツール、環境変数、ロードマップ、開発については [docs/cli.md](../docs/cli.md)（英語）を参照してください。[Antigravity ログインの詳細](../docs/antigravity.md)と[プロトコル仕様](../docs/protocol-contract.md)もあります。

貢献の方法は [CONTRIBUTING.md](../CONTRIBUTING.md)、脆弱性の非公開報告は [SECURITY.md](../SECURITY.md)、行動規範は [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md) を参照してください。

MIT License
