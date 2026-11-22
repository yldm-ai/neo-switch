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

Команда также показывает ссылку на случай, если открытым браузером воспользоваться нельзя, например на компьютере, к которому вы подключаетесь по SSH: откройте ссылку в браузере на любом устройстве и войдите. В Claude Code 2.1.326 и новее ссылка появляется сразу, а диалог просит вставить код, который покажет страница. В более старых версиях Claude Code, а также в WSL и в приложении для компьютера через SSH ссылку покажет ответ Claude: тогда выполните `/neoswitch:login` с этим кодом. Чтобы перейти на другой аккаунт Google, выполните `/neoswitch:login` ещё раз, не выходя из текущего: он остаётся до завершения входа, а `/neoswitch:login cancel` его сохраняет.

> [!WARNING]
> [Условия использования Google Antigravity](https://antigravity.google/terms) (раздел 6) запрещают пользоваться Antigravity через другие инструменты, а NeoSwitch как раз такой инструмент. Google может заблокировать или закрыть аккаунты Antigravity тех, кто так делает. `/neoswitch:login` каждый раз повторяет это предупреждение. Не входите, если не готовы принять этот риск.

**3. Отправьте следующее сообщение.** Окно, в котором вы выполнили команду, отвечает с помощью Gemini 4 Neon начиная со следующего сообщения. Другие окна и новые сессии продолжают работать с Claude, с вашим собственным входом в Claude и коннекторами: выполните `/neoswitch:on` в каждой сессии, которая должна отвечать с помощью Gemini 4 Neon. Если ответ команды просит перезапуститься, потому что Claude Code запущен через шлюз, снова запустите `claude` в том же проекте без шлюза.

## Что вы увидите

- Когда NeoSwitch настроен, новая сессия, которая его не использует, начинается не чаще раза в день для каждого проекта с уведомлением `NeoSwitch is set up: /neoswitch:on answers this session with Gemini 4 Neon, and /neoswitch:off returns it to Claude any time.` В проектах, где вы выполнили `/neoswitch:off`, оно не появляется; `NEOSWITCH_NO_HINT=1` в вашем окружении его отключает.
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
| Каждое сообщение завершается ошибкой соединения, или уведомление сессии говорит, что эта сессия не может отвечать | Выполните `/neoswitch:off`: эта сессия продолжит работу с Claude со следующего сообщения, даже если ответ на саму команду не придёт. Если сообщения по-прежнему не проходят, введите `/exit` и снова запустите `claude`: новая сессия использует Claude, а `/neoswitch:status` подскажет, что исправить. |
| Сообщение говорит, что другие окна Claude Code всё ещё используют прежнюю настройку | Закройте все окна Claude Code, снова откройте Claude Code и выполните `/neoswitch:on`. |
| Предупреждение говорит, что коннекторы claude.ai отключены, потому что задан другой источник авторизации | Нынешний NeoSwitch этого не вызывает; так делала прежняя версия. Обновите плагин (см. ниже). Если предупреждение остаётся, другой инструмент задаёт для Claude Code `ANTHROPIC_AUTH_TOKEN` или `ANTHROPIC_API_KEY`. |
| Сообщение говорит, что порт, нужный NeoSwitch, занят другой программой | Закройте эту программу и снова запустите Claude Code или выполните `/neoswitch:off`. |
| Сообщение говорит, что NeoSwitch не смог скачать свою программу | Проверьте подключение к интернету и снова запустите Claude Code. |
| `/neoswitch:login` — неизвестная команда | Обновите плагин (см. ниже), перезапустите Claude Code и выполните `/reload-plugins`. |
| Сообщение говорит, что тариф Antigravity вашего аккаунта Google не включает gemini-4-neon | Ваш тариф не включает Gemini 4 Neon, поэтому NeoSwitch не может отвечать им и оставляет проект на Claude. Выполните `/neoswitch:login` ещё раз, чтобы войти в аккаунт, тариф которого его включает, или выполните `/neoswitch:off` и начните новую сессию, чтобы вернуться к Claude. |

## Выключить, обновить или удалить

- `/neoswitch:off` возвращает эту сессию к Claude со следующего сообщения, с вашим собственным входом в Claude. `/neoswitch:on` снова переключает её на Gemini 4 Neon без повторной настройки. Закрытие сессии завершает её переключение: каждая новая сессия начинается с Claude.

Чтобы обновить вручную, выполните в терминале следующее, затем закройте все окна Claude Code и откройте их снова:

```bash
claude plugin marketplace update neoswitch
claude plugin update neoswitch@neoswitch
```

Если вы добавляли маркетплейс под старым именем `yldm-ai/NeoSwitch` / `yldm-ai/neon-switch`, сначала добавьте его заново командой `claude plugin marketplace add yldm-ai/neo-switch`.

Чтобы удалить NeoSwitch:

1. Сначала выполните `/neoswitch:off` в каждом проекте, который вы переключили. Проект, оставленный включённым, продолжит указывать на NeoSwitch и перестанет отвечать, когда NeoSwitch не станет. Если плагин уже удалён, откройте `.claude/settings.local.json` в каждом таком проекте и удалите `ANTHROPIC_BASE_URL`, `ANTHROPIC_CUSTOM_HEADERS`, `NODE_EXTRA_CA_CERTS`, `CLAUDE_CODE_USE_GATEWAY` и, от прежних версий, `ANTHROPIC_AUTH_TOKEN` из объекта `env`.
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

Пока сессия в проекте использует Gemini 4 Neon, NeoSwitch добавляет три записи в объект `env` файла `.claude/settings.local.json` этого проекта и удаляет их, когда ни одна сессия там его больше не использует:

- `ANTHROPIC_BASE_URL`, `ANTHROPIC_CUSTOM_HEADERS` и `NODE_EXTRA_CA_CERTS` направляют запросы Claude Code этого проекта в NeoSwitch на этом компьютере по зашифрованному соединению, на которое может ответить только NeoSwitch. Переключённым сессиям NeoSwitch отвечает с помощью Gemini 4 Neon, а все остальные запросы передаёт в Claude без изменений, с вашим собственным входом в Claude, который он не хранит и не записывает. Заголовок содержит ключ, открывающий только NeoSwitch на этом компьютере.

В Git-проекте NeoSwitch также добавляет `.claude/settings.local.json` в собственный список исключений клона (`.git/info/exclude`), чтобы токен не попал в коммиты; ваш `.gitignore` не меняется.

NeoSwitch работает только на этом компьютере и принимает подключения только с него. Он не записывает промпты, ответы, ключи и данные входа, и никакой наш сервер не участвует. Вход в Google остаётся внутри `agy`; NeoSwitch никогда его не читает.

## Подробнее

Что работает и что нет, как устроен NeoSwitch, отдельная утилита командной строки, переменные окружения, планы и разработка: [docs/cli.md](../docs/cli.md) (на английском). Также: [подробности о входе через Antigravity](../docs/antigravity.md) и [описание протокола](../docs/protocol-contract.md).

Как внести вклад — в [CONTRIBUTING.md](../CONTRIBUTING.md), как конфиденциально сообщить об уязвимости — в [SECURITY.md](../SECURITY.md), кодекс поведения — в [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md).

MIT License
