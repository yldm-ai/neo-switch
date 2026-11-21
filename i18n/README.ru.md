# NeoSwitch

[English](../README.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Español](README.es.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Português (Brasil)](README.pt-BR.md) | **Русский**

[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-D97757)](https://github.com/yldm-ai/neo-switch/blob/main/.claude-plugin/plugin.json) [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/yldm-ai/neo-switch/blob/main/LICENSE)

С NeoSwitch в Claude Code отвечает модель Google Gemini 4 Neon вместо Claude, а всё остальное в Claude Code остаётся прежним.

[![Настройка NeoSwitch в три шага](../docs/media/neoswitch-demo.gif)](../docs/media/neoswitch-demo.mp4)

Переключается по одному проекту; остальные проекты продолжают работать с Claude. Плагин работает в macOS и Linux. Windows пока не поддерживается.

## Начало работы

**1. Установите плагин.** В Claude Code выполните эти две строки по очереди. Копируйте каждую строку целиком: в первой написано `neo-switch`, во второй `neoswitch`.

```text
/plugin marketplace add yldm-ai/neo-switch
```

```text
/plugin install neoswitch@neoswitch
```

Затем включите автообновление, чтобы исправления приходили сами, когда Google или Claude Code меняют то, на что опирается NeoSwitch: выполните `/plugin`, откройте вкладку **Marketplaces**, выберите `neoswitch` и включите auto-update. Для маркетплейсов не от Anthropic Claude Code оставляет его выключенным.

**2. Войдите через аккаунт Google.** Откройте Claude Code в проекте, который хотите переключить, и выполните:

```text
/neoswitch:login
```

Команда покажет предупреждение об условиях Google, откроет вход в Google в браузере через [инструмент командной строки Google Antigravity (`agy`)](https://antigravity.google/download), а затем переключит проект. Если `agy` на этом компьютере ещё нет, команда сначала установит его в `~/.local/bin` официальным установщиком Google (около 230 МБ; установщик также добавит эту папку в PATH вашей оболочки). Если вы уверенно работаете в терминале, подойдёт и ключ Gemini API: см. [docs/cli.md](../docs/cli.md#gemini-api-key) (на английском).

Команда также показывает ссылку на случай, если открытым браузером воспользоваться нельзя, например на компьютере, к которому вы подключаетесь по SSH: откройте ссылку в браузере на любом устройстве, войдите и выполните `/neoswitch:login` с кодом, который покажет страница. Чтобы перейти на другой аккаунт Google, выполните `/neoswitch:login` ещё раз, не выходя из текущего: он остаётся до завершения входа, а `/neoswitch:login cancel` его сохраняет.

> [!WARNING]
> [Условия использования Google Antigravity](https://antigravity.google/terms) (раздел 6) запрещают пользоваться Antigravity через другие инструменты, а NeoSwitch как раз такой инструмент. Google может заблокировать или закрыть аккаунты Antigravity тех, кто так делает. `/neoswitch:login` каждый раз повторяет это предупреждение. Не входите, если не готовы принять этот риск.

**3. Отправьте следующее сообщение.** Окно, в котором вы выполнили команду, использует Gemini 4 Neon начиная со следующего сообщения, как и новые сессии этого проекта. Если ответ команды просит вместо этого перезапуститься, потому что Claude Code запущен через шлюз или другие окна Claude Code ещё используют прежнюю настройку NeoSwitch, закройте указанные окна и снова запустите `claude` в том же проекте. Когда закрывается последняя сессия здесь, которая использует NeoSwitch, проект возвращается к Claude, и следующая сессия начинается с Claude; чтобы снова использовать Gemini 4 Neon, выполните `/neoswitch:on`.

## Что вы увидите

- Новая сессия начинается с сообщения вроде `NeoSwitch: this session uses Gemini 4 Neon through your Google account (Antigravity sign-in). Ignore the Claude model name in the header. The first reply can take a while.`
- В заголовке по-прежнему видно название модели Claude, например `claude-opus-5-5`. Это только надпись: отвечает Gemini 4 Neon. Выбор другой модели через `/model` этого не меняет.
- До первых слов ответа может пройти от 5 до 40 секунд. Это нормально. Если вы вошли через `/neoswitch:login`, с `/effort low` ответы начинаются быстрее.
- Первая команда NeoSwitch после установки или обновления может занять до минуты, пока NeoSwitch готовится.
- `/neoswitch:status` в любой момент скажет, использует ли эта сессия Claude или Gemini 4 Neon и что делать дальше.
- Когда выходит новая версия NeoSwitch, заметка в начале сессии сообщает об этом и показывает команды для обновления. С включённым auto-update Claude Code ставит её сам; она начинает работать, когда вы закроете все окна Claude Code и откроете их снова.

## Если что-то пошло не так

| Что вы видите | Что делать |
|---|---|
| Сообщение в начале сессии говорит, что NeoSwitch ещё не настроен | Выполните `/neoswitch:login`. Следующее сообщение уже использует Gemini 4 Neon. |
| Сообщение говорит, что `agy` не установлен | `/neoswitch:login` не смог установить его сам. Выполните в терминале `curl -fsSL https://antigravity.google/cli/install.sh \| bash` или установите его со [страницы загрузки Google](https://antigravity.google/download), затем снова выполните `/neoswitch:login`. |
| Каждое сообщение завершается ошибкой соединения, или сообщение в начале сессии говорит, что эта сессия не может отвечать | В этом же проекте выполните `/neoswitch:off`. Он переключает проект обратно, даже если его собственный ответ тоже показывает ошибку соединения. Затем введите `/exit` и снова запустите `claude`: вы снова на Claude, а `/neoswitch:status` скажет, что исправить. |
| Сообщение говорит, что другие окна Claude Code всё ещё используют прежнюю настройку | Закройте все окна Claude Code, снова откройте Claude Code и выполните `/neoswitch:on`. |
| Предупреждение говорит, что коннекторы claude.ai отключены, потому что задан другой источник авторизации | Это ожидаемо, пока эта сессия использует NeoSwitch: его локальный ключ доступа важнее вашего входа в claude.ai. Коннекторы вернутся в следующей сессии, когда проект вернётся к Claude: после закрытия последней сессии с NeoSwitch или после `/neoswitch:off`. |
| Сообщение говорит, что порт, нужный NeoSwitch, занят другой программой | Закройте эту программу и снова запустите Claude Code или выполните `/neoswitch:off`. |
| Сообщение говорит, что NeoSwitch не смог скачать свою программу | Проверьте подключение к интернету и снова запустите Claude Code. |
| `/neoswitch:login` — неизвестная команда | Обновите плагин (см. ниже), перезапустите Claude Code и выполните `/reload-plugins`. |
| Сообщение говорит, что тариф Antigravity вашего аккаунта Google не включает gemini-4-neon | Ваш тариф не включает Gemini 4 Neon, поэтому NeoSwitch не может отвечать им и оставляет проект на Claude. Выполните `/neoswitch:login` ещё раз, чтобы войти в аккаунт, тариф которого его включает, или выполните `/neoswitch:off` и начните новую сессию, чтобы вернуться к Claude. |

## Выключить, обновить или удалить

- `/neoswitch:off` возвращает этот проект на Claude для новых сессий; сессия, которая уже использует Gemini 4 Neon, остаётся на нём, пока вы не введёте `/exit`. `/neoswitch:on` снова переключает проект на Gemini 4 Neon без повторной настройки, начиная со следующего сообщения. Закрытие последней сессии, использующей NeoSwitch в проекте, делает то же самое.

Чтобы обновить вручную, выполните в терминале следующее, затем закройте все окна Claude Code и откройте их снова:

```bash
claude plugin marketplace update neoswitch
claude plugin update neoswitch@neoswitch
```

Если вы добавляли маркетплейс под старым именем `yldm-ai/NeoSwitch` / `yldm-ai/neon-switch`, сначала добавьте его заново командой `claude plugin marketplace add yldm-ai/neo-switch`.

Чтобы удалить NeoSwitch:

1. Сначала выполните `/neoswitch:off` в каждом проекте, который вы переключили. Проект, оставленный включённым, продолжит указывать на NeoSwitch и перестанет отвечать, когда NeoSwitch не станет. Если плагин уже удалён, откройте `.claude/settings.local.json` в каждом таком проекте и удалите `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN` и `CLAUDE_CODE_USE_GATEWAY` из объекта `env`.
2. Закройте окна Claude Code, использующие NeoSwitch, и выполните в терминале:

```bash
claude plugin uninstall neoswitch@neoswitch
claude plugin marketplace remove neoswitch
```

3. Удалите файлы, которые NeoSwitch хранит вне ваших проектов:

| Что | Где |
|---|---|
| Программа NeoSwitch, которую плагин скачивает или собирает | `$XDG_CACHE_HOME/neoswitch/` или `~/.cache/neoswitch/`, если `XDG_CACHE_HOME` не задан |
| Настройки и журнал: `proxy.json` (адрес, ключ доступа и выбранный способ) и `daemon.log` | `NEOSWITCH_STATE_DIR`, если задан; иначе `~/Library/Application Support/neoswitch/` в macOS и `$XDG_CONFIG_HOME/neoswitch/` или `~/.config/neoswitch/` в Linux |

Вход в Google хранит `agy`, а не NeoSwitch, и он остаётся на месте; сам `agy` тоже остаётся, даже если его установил `/neoswitch:login`.

## Что меняет NeoSwitch

В каждом включённом проекте NeoSwitch добавляет три записи в объект `env` файла `.claude/settings.local.json`, а `/neoswitch:off` или закрытие последней сессии, использующей там NeoSwitch, снова их удаляет:

- `ANTHROPIC_BASE_URL` и `ANTHROPIC_AUTH_TOKEN` направляют новые сессии Claude Code в этом проекте к NeoSwitch на этом компьютере. Токен открывает только NeoSwitch на этом компьютере; это не учётные данные Google или Anthropic.
- `CLAUDE_CODE_USE_GATEWAY=0` заставляет эти сессии обращаться к NeoSwitch напрямую. Некоторые инструменты, запускающие Claude Code через другой шлюз, включают режим шлюза, и тогда Claude Code проигнорировал бы NeoSwitch.

В Git-проекте NeoSwitch также добавляет `.claude/settings.local.json` в собственный список исключений клона (`.git/info/exclude`), чтобы токен не попал в коммиты; ваш `.gitignore` не меняется.

NeoSwitch работает только на этом компьютере и принимает подключения только с него. Он не записывает промпты, ответы, ключи и данные входа, и никакой наш сервер не участвует. Вход в Google остаётся внутри `agy`; NeoSwitch никогда его не читает.

## Подробнее

Что работает и что нет, как устроен NeoSwitch, отдельная утилита командной строки, переменные окружения, планы и разработка: [docs/cli.md](../docs/cli.md) (на английском). Также: [подробности о входе через Antigravity](../docs/antigravity.md) и [описание протокола](../docs/protocol-contract.md).

Как внести вклад — в [CONTRIBUTING.md](../CONTRIBUTING.md), как конфиденциально сообщить об уязвимости — в [SECURITY.md](../SECURITY.md), кодекс поведения — в [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md).

MIT License
