# NeoSwitch

[English](../README.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md) | [日本語](README.ja.md) | **한국어** | [Español](README.es.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Português (Brasil)](README.pt-BR.md) | [Русский](README.ru.md)

[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-D97757)](https://github.com/yldm-ai/neo-switch/blob/main/.claude-plugin/plugin.json) [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/yldm-ai/neo-switch/blob/main/LICENSE)

NeoSwitch를 쓰면 Claude Code는 그대로 두고, Claude 대신 Google의 Gemini 4 Neon 모델이 답합니다.

[![세 단계로 NeoSwitch 설정하기](../docs/media/neoswitch-demo.gif)](../docs/media/neoswitch-demo.mp4)

프로젝트마다 따로 전환하며, 다른 프로젝트는 계속 Claude를 씁니다. 플러그인은 macOS와 Linux에서 동작합니다. Windows는 아직 지원하지 않습니다.

## 시작하기

**1. 플러그인을 설치합니다.** Claude Code에서 아래 두 줄을 한 줄씩 실행하세요. 각 줄은 통째로 복사하세요. 첫 줄은 `neo-switch`, 둘째 줄은 `neoswitch`입니다.

```text
/plugin marketplace add yldm-ai/neo-switch
```

```text
/plugin install neoswitch@neoswitch
```

이어서 자동 업데이트를 켜 두면 NeoSwitch가 의존하는 부분을 Google이나 Claude Code가 바꿨을 때도 수정이 자동으로 전달됩니다. `/plugin`을 실행하고 **Marketplaces** 탭에서 `neoswitch`를 선택한 뒤 auto-update를 켜세요. Anthropic 외의 마켓플레이스는 Claude Code가 기본적으로 자동 업데이트하지 않습니다.

**2. Google 계정으로 로그인합니다.** 전환할 프로젝트에서 Claude Code를 열고 다음을 실행하세요.

```text
/neoswitch:login
```

Google 약관 경고를 보여 준 뒤 Google의 [Antigravity 명령줄 도구(`agy`)](https://antigravity.google/download)를 통해 브라우저에서 Google 로그인을 열고, 마지막으로 이 프로젝트를 전환합니다. 이 컴퓨터에 아직 `agy`가 없으면 먼저 Google 공식 설치 스크립트로 `~/.local/bin`에 설치합니다(약 230MB. 설치 스크립트는 이 폴더를 셸의 PATH에도 추가합니다). 터미널에 익숙하다면 Gemini API 키도 쓸 수 있습니다. [docs/cli.md](../docs/cli.md#gemini-api-key)(영어)를 보세요.

SSH로 접속한 컴퓨터처럼 열린 브라우저를 쓸 수 없을 때를 위해 명령은 링크도 함께 보여 줍니다. 아무 기기의 브라우저에서 링크를 열어 로그인한 뒤, 페이지에 표시된 코드를 붙여 `/neoswitch:login`을 실행하세요. 로그인된 상태에서 `/neoswitch:login`을 다시 실행하면 다른 Google 계정으로 바꿀 수 있습니다. 끝날 때까지 현재 계정이 유지되며, `/neoswitch:login cancel`을 실행하면 그대로 유지됩니다.

> [!WARNING]
> [Google Antigravity 서비스 약관](https://antigravity.google/terms)(제6조)은 다른 도구를 통해 Antigravity를 쓰는 것을 금지하며, NeoSwitch는 그런 도구에 해당합니다. Google은 그렇게 쓰는 사람의 Antigravity 계정을 정지하거나 해지할 수 있습니다. `/neoswitch:login`은 매번 이 경고를 다시 보여 줍니다. 이 위험을 받아들일 수 없다면 로그인하지 마세요.

**3. 다음 메시지를 보냅니다.** 명령을 실행한 창은 다음 메시지부터 Gemini 4 Neon을 쓰고, 이 프로젝트의 새 세션도 마찬가지입니다. 명령의 답변이 대신 다시 시작하라고 하면(Claude Code를 게이트웨이로 시작했거나 다른 Claude Code 창이 아직 NeoSwitch의 이전 설정을 쓰는 경우), 안내된 창을 닫은 뒤 같은 프로젝트에서 `claude`를 다시 실행하세요. 이 프로젝트에서 NeoSwitch를 쓰는 마지막 세션을 닫으면 프로젝트가 Claude로 돌아가서 다음 세션은 Claude로 시작합니다. Gemini 4 Neon을 다시 쓰려면 `/neoswitch:on`을 실행하세요.

## 전환 후 보이는 것

- 새 세션이 시작될 때 `NeoSwitch: this session uses Gemini 4 Neon through your Google account (Antigravity sign-in). Ignore the Claude model name in the header. The first reply can take a while.` 같은 안내가 나옵니다.
- 헤더에는 `claude-opus-5-5` 같은 Claude 모델 이름이 그대로 보이지만 표시일 뿐이고, 실제로 답하는 것은 Gemini 4 Neon입니다. `/model`로 다른 모델을 골라도 바뀌지 않습니다.
- 답의 첫 글자가 나오기까지 5~40초가 걸릴 수 있으며 정상입니다. `/neoswitch:login`으로 로그인했다면 `/effort low`로 답이 더 빨리 시작됩니다.
- 설치나 업데이트 후 첫 NeoSwitch 명령은 준비 때문에 최대 1분쯤 걸릴 수 있습니다.
- `/neoswitch:status`를 실행하면 이 세션이 Claude와 Gemini 4 Neon 중 무엇을 쓰는지, 다음에 무엇을 하면 되는지 언제든 알려 줍니다.
- 새 NeoSwitch가 나오면 세션 시작 메시지로 알려 주고 업데이트 명령을 보여 줍니다. 자동 업데이트가 켜져 있으면 Claude Code가 직접 설치하며, Claude Code 창을 모두 닫았다가 다시 열면 적용됩니다.

## 문제가 생기면

| 보이는 내용 | 해결 방법 |
|---|---|
| 세션 시작 안내에 NeoSwitch가 아직 설정되지 않았다고 나옴 | `/neoswitch:login`을 실행하세요. 다음 메시지부터 Gemini 4 Neon을 씁니다. |
| `agy`가 설치되어 있지 않다는 메시지가 나옴 | `/neoswitch:login`이 자동으로 설치하지 못한 경우입니다. 터미널에서 `curl -fsSL https://antigravity.google/cli/install.sh \| bash`를 실행하거나 [Google 다운로드 페이지](https://antigravity.google/download)에서 설치한 뒤 `/neoswitch:login`을 다시 실행하세요. |
| 모든 메시지가 연결 오류로 실패하거나, 세션 시작 안내에 이 세션은 답할 수 없다고 나옴 | 같은 프로젝트에서 `/neoswitch:off`를 실행하세요. 그 응답에도 연결 오류가 보이더라도 프로젝트는 이미 되돌려졌습니다. `/exit`를 입력하고 `claude`를 다시 시작하면 Claude로 돌아오며, `/neoswitch:status`가 무엇을 고쳐야 하는지 알려 줍니다. |
| 다른 Claude Code 창이 이전 설정을 함께 쓰고 있다고 나옴 | Claude Code 창을 모두 닫고 다시 연 뒤 `/neoswitch:on`을 실행하세요. |
| 다른 인증 소스가 설정되어 claude.ai connectors가 비활성화되었다는 경고가 나온다 | 이 세션이 NeoSwitch를 쓰는 동안에는 정상입니다. 로컬 액세스 키가 claude.ai 로그인보다 우선합니다. 프로젝트가 Claude로 돌아가면(NeoSwitch를 쓰는 마지막 세션을 닫거나 `/neoswitch:off`를 실행하면) 다음 세션에서 connectors가 돌아옵니다. |
| NeoSwitch에 필요한 포트를 다른 프로그램이 쓰고 있다고 나옴 | 그 프로그램을 닫고 Claude Code를 다시 시작하거나 `/neoswitch:off`를 실행하세요. |
| NeoSwitch가 프로그램을 내려받지 못했다고 나옴 | 인터넷 연결을 확인하고 Claude Code를 다시 시작하세요. |
| `/neoswitch:login`이 알 수 없는 명령으로 나옴 | 플러그인을 업데이트하고(아래 참고) Claude Code를 다시 시작한 뒤 `/reload-plugins`를 실행하세요. |
| Google 계정의 Antigravity 요금제가 gemini-4-neon을 제공하지 않는다는 메시지가 나온다 | 요금제에 Gemini 4 Neon이 없어서 NeoSwitch가 그 모델로 답할 수 없고, 프로젝트는 Claude를 계속 씁니다. `/neoswitch:login`을 다시 실행해 요금제에 포함된 계정으로 로그인하거나, `/neoswitch:off`를 실행하고 새 세션을 시작해 Claude로 돌아가세요. |

## 끄기, 업데이트, 제거

- `/neoswitch:off`는 이 프로젝트의 새 세션을 Claude로 되돌립니다. 이미 Gemini 4 Neon을 쓰는 세션은 `/exit`를 입력할 때까지 그대로입니다. `/neoswitch:on`은 다시 설정하지 않고 다음 메시지부터 프로젝트를 Gemini 4 Neon으로 다시 전환합니다. 프로젝트에서 NeoSwitch를 쓰는 마지막 세션을 닫아도 같습니다.

직접 업데이트하려면 터미널에서 다음을 실행한 뒤 Claude Code 창을 모두 닫았다가 다시 여세요.

```bash
claude plugin marketplace update neoswitch
claude plugin update neoswitch@neoswitch
```

마켓플레이스를 예전 이름 `yldm-ai/NeoSwitch` / `yldm-ai/neon-switch`로 추가했다면 먼저 `claude plugin marketplace add yldm-ai/neo-switch`로 다시 추가하세요.

NeoSwitch를 제거하려면:

1. 전환한 각 프로젝트에서 먼저 `/neoswitch:off`를 실행하세요. 켜진 채로 남은 프로젝트는 계속 NeoSwitch를 가리키므로 NeoSwitch가 사라지면 답하지 못합니다. 플러그인을 이미 제거했다면 해당 프로젝트마다 `.claude/settings.local.json`을 열고 `env` 객체에서 `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN`, `CLAUDE_CODE_USE_GATEWAY`를 지우세요.
2. NeoSwitch를 쓰는 Claude Code 창을 닫고 터미널에서 다음을 실행하세요.

```bash
claude plugin uninstall neoswitch@neoswitch
claude plugin marketplace remove neoswitch
```

3. NeoSwitch가 프로젝트 밖에 보관하는 파일을 지우세요.

| 내용 | 위치 |
|---|---|
| 플러그인이 내려받거나 빌드한 NeoSwitch 프로그램 | `$XDG_CACHE_HOME/neoswitch/`, `XDG_CACHE_HOME`이 없으면 `~/.cache/neoswitch/` |
| 설정과 로그: `proxy.json`(주소, 접근 키, 고른 방법), `daemon.log` | `NEOSWITCH_STATE_DIR`가 설정되어 있으면 그곳, 아니면 macOS는 `~/Library/Application Support/neoswitch/`, Linux는 `$XDG_CONFIG_HOME/neoswitch/` 또는 `~/.config/neoswitch/` |

Google 로그인 정보는 NeoSwitch가 아니라 `agy`가 보관하며 그대로 남습니다. `/neoswitch:login`이 설치한 경우를 포함해 `agy` 자체도 남습니다.

## NeoSwitch가 바꾸는 것

켠 프로젝트마다 NeoSwitch는 `.claude/settings.local.json`의 `env` 객체에 세 항목을 추가하고, `/neoswitch:off`를 실행하거나 그 프로젝트에서 NeoSwitch를 쓰는 마지막 세션을 닫으면 다시 지웁니다.

- `ANTHROPIC_BASE_URL`과 `ANTHROPIC_AUTH_TOKEN`: 그 프로젝트의 새 Claude Code 세션을 이 컴퓨터의 NeoSwitch로 보냅니다. 이 토큰은 이 컴퓨터의 NeoSwitch에만 쓸 수 있으며 Google이나 Anthropic 자격 증명이 아닙니다.
- `CLAUDE_CODE_USE_GATEWAY=0`: 그 세션이 NeoSwitch에 직접 연결되게 합니다. 다른 게이트웨이를 통해 Claude Code를 실행하는 일부 도구는 게이트웨이 모드를 켜는데, 그러면 Claude Code가 NeoSwitch를 무시합니다.

Git 프로젝트에서는 NeoSwitch가 `.claude/settings.local.json`을 해당 클론 전용 제외 목록(`.git/info/exclude`)에도 추가해 토큰이 커밋에 들어가지 않게 합니다. `.gitignore`는 건드리지 않습니다.

NeoSwitch는 이 컴퓨터에서만 실행되고 이 컴퓨터에서 오는 연결만 받습니다. 프롬프트, 답변, 키, 로그인 정보를 기록하지 않으며 저희 서버를 거치지 않습니다. Google 로그인 정보는 `agy` 안에 있으며 NeoSwitch는 읽지 않습니다.

## 더 알아보기

되는 기능과 안 되는 기능, NeoSwitch의 동작 방식, 독립 실행형 명령줄 도구, 환경 변수, 로드맵, 개발은 [docs/cli.md](../docs/cli.md)(영어)를 보세요. [Antigravity 로그인 자세히](../docs/antigravity.md)와 [프로토콜 계약](../docs/protocol-contract.md)도 있습니다.

기여 방법은 [CONTRIBUTING.md](../CONTRIBUTING.md), 취약점 비공개 신고는 [SECURITY.md](../SECURITY.md), 행동 강령은 [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md)를 보세요.

MIT License
