# Contributing to NeoSwitch

Bug reports, fixes and documentation improvements are welcome. By taking part you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md). Report security issues privately as described in [SECURITY.md](SECURITY.md), not in public issues.

## Development setup

- Go 1.27 or newer, the version in [go.mod](go.mod).
- Claude Code, to try the plugin end to end.
- Optional: the official Antigravity CLI (`agy`) for the OAuth path, or a `GEMINI_API_KEY` or Vertex AI project for the Gemini path.

Build and run the CLI from the repository root:

```bash
go build -o neoswitch ./cmd/neoswitch
./neoswitch doctor
```

To try your checkout as a plugin, add the local directory as a marketplace inside Claude Code with `/plugin marketplace add /path/to/neo-switch`, then install `neoswitch@neoswitch`. The plugin builds the binary from that source on first use; after changing Go code, run `claude plugin marketplace update neoswitch` and `claude plugin update neoswitch@neoswitch`, then restart Claude Code.

## Before you open a pull request

Run the same checks as CI:

```bash
go test ./...
go test -race ./...
go vet ./...
```

- Add or update tests for behavior changes.
- A change to the plugin's mod (`plugin/mod/`) passes `claude plugin validate .`. To type-check it, run `claude --plugin-dir .` once, which writes the plugin API's types and a `tsconfig.json` beside the plugin (both ignored by git), then `npx -p typescript tsc --noEmit -p .`. Try the commands it answers in real Claude Code: unit tests cannot show what the dialog does.
- Use [Conventional Commits](https://www.conventionalcommits.org/) for commit messages and pull request titles, for example `fix: ...`, `feat: ...`, `docs: ...`, `test: ...`, `chore: ...`.
- One issue per pull request. Write `Fixes #N` in the description; unrelated changes go in separate pull requests.
- Keep [README.md](README.md) and the nine translations in [i18n/](i18n/) in sync. A README change goes into every translation in the same pull request, with the same structure: the same headings, code blocks, tables and line count, and links that resolve from `i18n/`.
- Write one paragraph per line in Markdown and comments; do not hard-wrap text.
- Never commit credentials: API keys, OAuth tokens, `.env` files, Application Default Credentials, or a `.claude/settings.local.json` that contains the proxy token.

## Releases

Maintainers release by pushing a tag that starts with `v`, such as `v0.2.0`. The [release workflow](.github/workflows/release.yml) runs `go test -race ./...` and `go vet ./...`, builds the `neoswitch` binaries for macOS, Linux and Windows with the tag as the version, and publishes a GitHub Release with the binaries and a `checksums.txt` file of SHA-256 sums.

Most users get NeoSwitch through the plugin marketplace. [`.claude-plugin/marketplace.json`](.claude-plugin/marketplace.json) pins the plugin to a release tag (`"ref": "v0.4.0"`), so users get exactly the tree that release was built from: the plugin's skills, hooks and mod always match the binary that plugin users without Go download, and changes merged to `main` reach users only with the next release. The session note tells plugin users to update when `main`'s `VERSION` is newer than the version they run, so `VERSION` and the pin move together, and only to a published release:

1. Merge the changes to `main`. A change that users should get, above all a fix for a protocol change upstream, needs a release: until then no user gets it.
2. On a new branch from the latest `main`, change `VERSION` and the pin in `.claude-plugin/marketplace.json` to the new version (the pin is `v` followed by `VERSION`), commit, and push the branch.
3. Push the matching tag on that commit and wait for the release workflow to publish the release.
4. Open the pull request and merge it. Other pull requests may merge before it in any order: users get the tagged tree, not the merged one. The `released-version` check fails when the pin and `VERSION` differ, while the tag is missing or its release is not published, or when the tag's own `VERSION` differs; re-run it after the release is published.

To take a release back, open a pull request that moves `VERSION` and the pin to an earlier published release, or release a fix as a new version. A release that failed to publish is fixed or its branch dropped the same day: nothing reaches users before its pull request merges.
