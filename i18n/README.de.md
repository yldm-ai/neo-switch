# NeoSwitch

[English](../README.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Español](README.es.md) | [Français](README.fr.md) | **Deutsch** | [Português (Brasil)](README.pt-BR.md) | [Русский](README.ru.md)

[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-D97757)](https://github.com/yldm-ai/neo-switch/blob/main/.claude-plugin/plugin.json) [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/yldm-ai/neo-switch/blob/main/LICENSE)

Mit NeoSwitch antwortet in Claude Code das Modell Gemini 4 Neon von Google statt Claude, während alles andere in Claude Code gleich bleibt.

[![NeoSwitch in drei Schritten einrichten](../docs/media/neoswitch-demo.gif)](../docs/media/neoswitch-demo.mp4)

Sie schalten jeweils ein Projekt um; Ihre anderen Projekte nutzen weiter Claude. Das Plugin läuft unter macOS und Linux. Windows wird noch nicht unterstützt.

## Loslegen

**1. Plugin installieren.** Führen Sie in Claude Code diese beiden Zeilen nacheinander aus. Kopieren Sie jede Zeile vollständig: In der ersten steht `neo-switch`, in der zweiten `neoswitch`.

```text
/plugin marketplace add yldm-ai/neo-switch
```

```text
/plugin install neoswitch@neoswitch
```

Schalten Sie danach automatische Updates ein, damit Korrekturen Sie erreichen, wenn Google oder Claude Code etwas ändern, worauf NeoSwitch angewiesen ist: Führen Sie `/plugin` aus, öffnen Sie den Tab **Marketplaces**, wählen Sie `neoswitch` und schalten Sie auto-update ein. Für Marketplaces, die nicht von Anthropic stammen, lässt Claude Code es ausgeschaltet.

**2. Mit Ihrem Google-Konto anmelden.** Öffnen Sie Claude Code in dem Projekt, das Sie umschalten möchten, und führen Sie aus:

```text
/neoswitch:login
```

Der Befehl zeigt Googles Warnung zu den Nutzungsbedingungen, öffnet die Google-Anmeldung in Ihrem Browser über Googles [Antigravity-Befehlszeilentool (`agy`)](https://antigravity.google/download) und schaltet dann das Projekt um. Ist `agy` auf diesem Computer noch nicht installiert, installiert der Befehl es zuerst mit Googles offiziellem Installer nach `~/.local/bin` (etwa 230 MB; der Installer trägt diesen Ordner auch in den PATH Ihrer Shell ein). Wer mit dem Terminal vertraut ist, kann auch einen Gemini-API-Schlüssel verwenden: siehe [docs/cli.md](../docs/cli.md#gemini-api-key) (auf Englisch).

Der Befehl zeigt außerdem einen Link, falls Sie den geöffneten Browser nicht nutzen können, etwa auf einem Computer, den Sie per SSH erreichen: Öffnen Sie den Link in einem Browser auf einem beliebigen Gerät und melden Sie sich an. Ab Claude Code 2.1.326 erscheint der Link sofort, und ein Dialog fragt nach dem Code, den die Seite anzeigt: Fügen Sie ihn dort ein. In älteren Versionen von Claude Code sowie in WSL oder der Desktop-App über SSH zeigt Claudes Antwort den Link: Führen Sie dann `/neoswitch:login` gefolgt von dem Code aus. Führen Sie `/neoswitch:login` im angemeldeten Zustand erneut aus, um zu einem anderen Google-Konto zu wechseln: Das aktuelle bleibt, bis Sie fertig sind, und `/neoswitch:login cancel` behält es.

> [!WARNING]
> Die [Nutzungsbedingungen von Google Antigravity](https://antigravity.google/terms) (Abschnitt 6) verbieten es, Antigravity über andere Tools zu nutzen, und NeoSwitch ist ein solches Tool. Google kann die Antigravity-Konten von Personen, die das tun, sperren oder schließen. `/neoswitch:login` wiederholt diese Warnung jedes Mal. Melden Sie sich nicht an, wenn Sie dieses Risiko nicht akzeptieren.

**3. Senden Sie Ihre nächste Nachricht.** Das Fenster, in dem Sie den Befehl ausgeführt haben, nutzt ab seiner nächsten Nachricht Gemini 4 Neon. Andere Fenster und neue Sitzungen nutzen weiter Claude, mit Ihrer eigenen Claude-Anmeldung und Ihren Connectors: Führen Sie `/neoswitch:on` in jeder Sitzung aus, die mit Gemini 4 Neon antworten soll. Wenn die Antwort des Befehls Sie stattdessen zum Neustart auffordert, weil Claude Code über ein Gateway gestartet wurde, starten Sie `claude` im selben Projekt ohne das Gateway erneut.

## Was Sie sehen werden

- Sobald NeoSwitch eingerichtet ist, beginnt eine neue Sitzung, die es nicht nutzt, höchstens einmal am Tag pro Projekt mit dem Hinweis `NeoSwitch is set up: /neoswitch:on answers this session with Gemini 4 Neon, and /neoswitch:off returns it to Claude any time.` In Projekten, in denen Sie `/neoswitch:off` ausgeführt haben, erscheint er nicht; `NEOSWITCH_NO_HINT=1` in Ihrer Umgebung schaltet ihn ab.
- Die Kopfzeile zeigt weiterhin einen Claude-Modellnamen wie `claude-opus-5-5`. Das ist nur eine Beschriftung: Es antwortet Gemini 4 Neon. Ein anderes Modell mit `/model` zu wählen ändert daran nichts.
- Eine Antwort kann 5 bis 40 Sekunden brauchen, bis die ersten Wörter erscheinen. Das ist normal. Wenn Sie sich mit `/neoswitch:login` angemeldet haben, beginnen Antworten mit `/effort low` früher.
- Der erste NeoSwitch-Befehl nach einer Installation oder einem Update kann bis zu einer Minute dauern, während sich NeoSwitch vorbereitet.
- `/neoswitch:status` sagt Ihnen jederzeit, ob diese Sitzung Claude oder Gemini 4 Neon nutzt und was als Nächstes zu tun ist.
- Wenn eine neuere NeoSwitch-Version verfügbar ist, sagt die Sitzungsnotiz das und nennt die Befehle zum Aktualisieren. Mit eingeschaltetem auto-update installiert Claude Code sie selbst, und die schon geöffneten Fenster wechseln zu ihr, sobald eine neue Claude-Code-Sitzung startet. Unter Windows gilt sie, sobald alle Claude-Code-Fenster geschlossen und wieder geöffnet wurden.

## Wenn etwas nicht klappt

| Was Sie sehen | Was zu tun ist |
|---|---|
| Der Sitzungshinweis sagt, NeoSwitch sei noch nicht eingerichtet | Führen Sie `/neoswitch:login` aus. Ihre nächste Nachricht nutzt Gemini 4 Neon. |
| Eine Meldung sagt, `agy` sei nicht installiert | `/neoswitch:login` konnte es nicht selbst installieren. Führen Sie `curl -fsSL https://antigravity.google/cli/install.sh \| bash` in einem Terminal aus oder installieren Sie es über [Googles Download-Seite](https://antigravity.google/download), und führen Sie dann `/neoswitch:login` erneut aus. |
| Jede Nachricht scheitert mit einem Verbindungsfehler, oder der Sitzungshinweis sagt, diese Sitzung könne nicht antworten | Führen Sie `/neoswitch:off` aus: Diese Sitzung arbeitet ab ihrer nächsten Nachricht mit Claude weiter, auch wenn die Antwort auf den Befehl selbst scheitert. Scheitern Nachrichten weiter, geben Sie `/exit` ein und starten Sie `claude` erneut: Eine neue Sitzung nutzt Claude, und `/neoswitch:status` sagt Ihnen, was zu beheben ist. |
| Der Hinweis sagt, andere Claude-Code-Fenster teilten noch die bisherige Einrichtung | Führen Sie `/neoswitch:on` in dieser Sitzung aus: alle geöffneten Fenster wechseln zu ihrer Einrichtung. Unter Windows schließen Sie alle Claude-Code-Fenster, öffnen Claude Code erneut und führen `/neoswitch:on` aus. |
| Eine Warnung sagt, claude.ai-Connectors seien deaktiviert, weil eine andere Anmeldequelle gesetzt ist | NeoSwitch verursacht das nicht mehr; eine frühere Version tat es. Aktualisieren Sie das Plugin (siehe unten). Bleibt die Warnung, setzt ein anderes Werkzeug `ANTHROPIC_AUTH_TOKEN` oder `ANTHROPIC_API_KEY` für Claude Code. |
| Der Hinweis sagt, ein anderes Programm belege den Port, den NeoSwitch braucht | Schließen Sie dieses Programm und starten Sie Claude Code erneut, oder führen Sie `/neoswitch:off` aus. |
| Der Hinweis sagt, NeoSwitch habe sein Programm nicht herunterladen können | Prüfen Sie Ihre Internetverbindung und starten Sie Claude Code erneut. |
| `/neoswitch:login` ist ein unbekannter Befehl | Aktualisieren Sie das Plugin (siehe unten), starten Sie Claude Code neu und führen Sie `/reload-plugins` aus. |
| Eine Meldung sagt, der Antigravity-Tarif Ihres Google-Kontos biete gemini-4-neon nicht an | Ihr Tarif enthält Gemini 4 Neon nicht, daher kann NeoSwitch damit nicht antworten und lässt das Projekt bei Claude. Führen Sie `/neoswitch:login` erneut aus, um sich mit einem Konto anzumelden, dessen Tarif es enthält, oder führen Sie `/neoswitch:off` aus und starten Sie eine neue Sitzung, um zu Claude zurückzukehren. |

## Ausschalten, aktualisieren oder entfernen

- `/neoswitch:off` schaltet diese Sitzung ab ihrer nächsten Nachricht zurück auf Claude, mit Ihrer eigenen Claude-Anmeldung. `/neoswitch:on` schaltet sie wieder auf Gemini 4 Neon, ohne dass Sie etwas neu einrichten. Mit dem Schließen einer Sitzung endet ihre Umschaltung: Jede neue Sitzung beginnt mit Claude.

Um von Hand zu aktualisieren, führen Sie dies in einem Terminal aus und öffnen Sie dann eine neue Claude-Code-Sitzung: die schon geöffneten Fenster wechseln ebenfalls zur neuen Version (unter Windows schließen Sie alle Claude-Code-Fenster und öffnen sie wieder):

```bash
claude plugin marketplace update neoswitch
claude plugin update neoswitch@neoswitch
```

Wenn Sie den Marketplace unter seinem alten Namen `yldm-ai/NeoSwitch` / `yldm-ai/neon-switch` hinzugefügt haben, fügen Sie ihn zuerst mit `claude plugin marketplace add yldm-ai/neo-switch` erneut hinzu.

So entfernen Sie NeoSwitch:

1. Führen Sie zuerst in jedem Projekt, das Sie umgeschaltet haben, `/neoswitch:off` aus. Ein eingeschaltet gelassenes Projekt zeigt weiter auf NeoSwitch und antwortet nicht mehr, sobald NeoSwitch entfernt ist. Wenn Sie das Plugin schon entfernt haben, öffnen Sie in jedem solchen Projekt `.claude/settings.local.json` und löschen Sie `ANTHROPIC_BASE_URL`, `ANTHROPIC_CUSTOM_HEADERS`, `NODE_EXTRA_CA_CERTS`, `CLAUDE_CODE_USE_GATEWAY` und, von früheren Versionen, `ANTHROPIC_AUTH_TOKEN` aus dem `env`-Objekt.
2. Schließen Sie die Claude-Code-Fenster, die NeoSwitch nutzen, und führen Sie dies in einem Terminal aus:

```bash
claude plugin uninstall neoswitch@neoswitch
claude plugin marketplace remove neoswitch
```

3. Löschen Sie die Dateien, die NeoSwitch außerhalb Ihrer Projekte aufbewahrt:

| Was | Wo |
|---|---|
| Das NeoSwitch-Programm, das das Plugin herunterlädt oder baut | `$XDG_CACHE_HOME/neoswitch/`, oder `~/.cache/neoswitch/`, wenn `XDG_CACHE_HOME` nicht gesetzt ist |
| Einstellungen und Protokoll: `proxy.json` (Adresse, Zugangsschlüssel und gewählter Weg) und `daemon.log` | `NEOSWITCH_STATE_DIR`, wenn gesetzt; sonst `~/Library/Application Support/neoswitch/` unter macOS und `$XDG_CONFIG_HOME/neoswitch/` oder `~/.config/neoswitch/` unter Linux |

Die Google-Anmeldung speichert `agy`, nicht NeoSwitch, und sie bleibt bestehen; ebenso `agy` selbst, auch wenn `/neoswitch:login` es installiert hat.

## Was NeoSwitch ändert

Solange eine Sitzung in einem Projekt Gemini 4 Neon nutzt, fügt NeoSwitch dem `env`-Objekt der `.claude/settings.local.json` dieses Projekts drei Einträge hinzu und entfernt sie, sobald keine Sitzung dort es mehr nutzt:

- `ANTHROPIC_BASE_URL`, `ANTHROPIC_CUSTOM_HEADERS` und `NODE_EXTRA_CA_CERTS` schicken die Claude-Code-Anfragen des Projekts über eine verschlüsselte Verbindung, die nur NeoSwitch beantworten kann, an NeoSwitch auf diesem Computer. NeoSwitch beantwortet die umgeschalteten Sitzungen mit Gemini 4 Neon und reicht alle anderen Anfragen unverändert an Claude weiter, mit Ihrer eigenen Claude-Anmeldung, die es weder speichert noch protokolliert. Der Header enthält einen Schlüssel, der nur NeoSwitch auf diesem Computer öffnet.

In einem Git-Projekt trägt NeoSwitch `.claude/settings.local.json` außerdem in die klon-eigene Ausschlussliste (`.git/info/exclude`) ein, damit das Token nicht in Commits landet; Ihre `.gitignore` bleibt unverändert.

NeoSwitch läuft nur auf diesem Computer und nimmt nur Verbindungen von ihm an. Es protokolliert keine Prompts, Antworten, Schlüssel oder Anmeldedaten, und kein Server von uns ist beteiligt. Die Google-Anmeldung bleibt in `agy`; NeoSwitch liest sie nie.

## Mehr

Was funktioniert und was nicht, wie NeoSwitch funktioniert, das eigenständige Kommandozeilenwerkzeug, Umgebungsvariablen, Roadmap und Entwicklung: [docs/cli.md](../docs/cli.md) (auf Englisch). Außerdem: [Details zur Antigravity-Anmeldung](../docs/antigravity.md) und der [Protokollvertrag](../docs/protocol-contract.md).

Siehe [CONTRIBUTING.md](../CONTRIBUTING.md) zum Mitwirken, [SECURITY.md](../SECURITY.md) zum vertraulichen Melden einer Sicherheitslücke und [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md) für den Verhaltenskodex.

MIT License
