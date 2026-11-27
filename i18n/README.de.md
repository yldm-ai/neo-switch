# NeoSwitch

[English](../README.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Español](README.es.md) | [Français](README.fr.md) | **Deutsch** | [Português (Brasil)](README.pt-BR.md) | [Русский](README.ru.md)

[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-D97757)](https://github.com/yldm-ai/neo-switch/blob/main/.claude-plugin/plugin.json) [![Release](https://img.shields.io/github/v/release/yldm-ai/neo-switch)](https://github.com/yldm-ai/neo-switch/releases/latest) [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/yldm-ai/neo-switch/blob/main/LICENSE)

Mit NeoSwitch können Sie einzelne Claude-Code-Sitzungen auf Googles Gemini 4 Neon umschalten, während Ihre Claude-Code-Einrichtung, Konnektoren, Berechtigungen und anderen Fenster unverändert bleiben.

[![NeoSwitch in drei Schritten einrichten](../docs/media/neoswitch-demo.gif)](../docs/media/neoswitch-demo.mp4)

Schalten Sie eine Sitzung auf einmal um — andere Fenster im selben Projekt nutzen weiter Claude. Das Plugin läuft unter macOS und Linux; Windows wird noch nicht unterstützt.

## Highlights

- **Eine Sitzung auf einmal, direkt vor Ort zurückschalten.** `/neoswitch:on` und `/neoswitch:off` wirken nur auf die aktuelle Sitzung ab der nächsten Nachricht; andere Fenster im selben Projekt und neue Sitzungen bleiben bei Claude, und beim Schließen der letzten umgeschalteten Sitzung entfernt NeoSwitch seine Projekteinträge automatisch.
- **Claude Code bleibt Claude Code.** Kein `ANTHROPIC_AUTH_TOKEN` wird geschrieben, sodass `claude.ai`-Konnektoren weiter funktionieren; das eingebaute `WebSearch` sucht unter Antigravity über Google; der Auto-Modus läuft direkt mit einem Fail-Closed-Sicherheitsklassifikator (ohne Neustart, null Claude-Verbrauch); `/effort` und optionale Thinking-Blöcke (`NEOSWITCH_SHOW_THINKING`) werden sauber auf Gemini abgebildet.
- **Sofortige native Befehle und Live-Kontingent.** Ab Claude Code 2.1.326 antworten `/neoswitch:login`, `/neoswitch:on`, `/neoswitch:off` und `/neoswitch:status` direkt ohne Modellrunde, eine Statuszeile unter dem Prompt zeigt das aktive Modell an, und `/neoswitch:status` zeigt 5-Stunden- und Wochen-Kontingentbalken samt Rücksetzzeiten ohne Kontingentverbrauch (`-v` ergänzt alle Kontingentgruppen und Credits).
- **Für lange reale Sitzungen gebaut.** ACP-Sitzungswiederverwendung und Chunking umgehen das 192-KB-Limit von `agy`, ein schlanker Custom-Agent senkt den `agy`-Prompt-Overhead von ~14k auf unter 1k Tokens, parallele Tool-Aufrufe reparieren JSON-Escapes, und Prompt-Vorschläge sowie Sitzungstitel laufen über `gemini-3.8-flash`, um Gemini-4-Neon-Kontingent zu sparen.
- **Lokal, isoliert, rückgängig machbar.** Der Verkehr bleibt auf lokalem TLS, dessen CA-Privatschlüssel nach dem Signieren verworfen wird; NeoSwitch liest, speichert oder wiederholt nie Google-OAuth-Tokens; `agy` läuft in einem isolierten Arbeitsordner, in dem seine eigenen Datei- und Shell-Tools sowohl durch den schlanken Agent als auch durch einen geprüften `PreToolUse`-Hook gesperrt sind; eigene `NODE_EXTRA_CA_CERTS` und `ANTHROPIC_CUSTOM_HEADERS` werden gesichert und beim Ausschalten wiederhergestellt.
- **SSH-tauglich und nahtlose Updates.** `/neoswitch:login` öffnet den Browser und gibt zusätzlich einen Link + Code für Remote-/SSH-Rechner aus, Plugin-Updates übergeben den Daemon-Port automatisch ohne offene Fenster zu schließen, und bei Build- oder Download-Fehlern springt eine zwischengespeicherte Version ein, damit `/neoswitch:off` immer einen Ausweg hat.
- **Mehrere Backends.** Nutzen Sie Ihr Google-Konto über das offizielle `agy`, oder verwenden Sie `GEMINI_API_KEY`, Vertex AI oder das experimentelle OpenAI-Responses-Backend.

## Schnellstart

**1. Plugin installieren.** Führen Sie in Claude Code diese beiden Zeilen nacheinander aus. Kopieren Sie jede Zeile vollständig: In der ersten steht `neo-switch`, in der zweiten `neoswitch`.

```text
/plugin marketplace add yldm-ai/neo-switch
```

```text
/plugin install neoswitch@neoswitch
```

Schalten Sie automatische Updates ein, damit Korrekturen Sie erreichen, wenn Google oder Claude Code etwas ändern, worauf NeoSwitch angewiesen ist: Führen Sie `/plugin` aus, öffnen Sie den Tab **Marketplaces**, wählen Sie `neoswitch` und schalten Sie auto-update ein. Für Drittanbieter-Marketplaces lässt Claude Code es standardmäßig aus.

**2. Mit Ihrem Google-Konto anmelden.** Öffnen Sie Claude Code in dem Projekt, das Sie umschalten möchten, und führen Sie aus:

```text
/neoswitch:login
```

Der Befehl zeigt Googles Warnung zu den Nutzungsbedingungen, installiert Googles offizielle [Antigravity-CLI (`agy`)](https://antigravity.google/download) bei Bedarf nach `~/.local/bin` (~230 MB), öffnet die Google-Anmeldung im Browser und schaltet diese Sitzung ein. Außerdem wird ein Link für Remote-/SSH-Rechner ausgegeben: Öffnen Sie ihn auf einem beliebigen Gerät, melden Sie sich an und fügen Sie den Code im Dialog von Claude Code 2.1.326+ ein (oder führen Sie `/neoswitch:login <code>` in älteren Versionen, WSL oder der Desktop-App über SSH aus). Führen Sie `/neoswitch:login` erneut aus, um das Google-Konto zu wechseln, oder `/neoswitch:login cancel`, um das aktuelle zu behalten. Lieber API-Schlüssel oder Vertex AI? Siehe [docs/cli.md](../docs/cli.md#gemini-api-key) (auf Englisch).

> [!WARNING]
> Die [Nutzungsbedingungen von Google Antigravity](https://antigravity.google/terms) (Abschnitt 6) verbieten es, Antigravity über andere Tools zu nutzen, und NeoSwitch ist ein solches Tool. Google kann die Antigravity-Konten von Personen, die das tun, sperren oder schließen. `/neoswitch:login` wiederholt diese Warnung jedes Mal. Melden Sie sich nicht an, wenn Sie dieses Risiko nicht akzeptieren.

**3. Senden Sie Ihre nächste Nachricht.** Diese Sitzung nutzt ab der nächsten Nachricht Gemini 4 Neon, während andere Fenster und neue Sitzungen bei Claude bleiben. Wenn die Antwort Sie zum Neustart auffordert, weil Claude Code über ein Gateway gestartet wurde, starten Sie `claude` im selben Projekt ohne das Gateway erneut.

| Befehl | Wirkung |
|---|---|
| `/neoswitch:login [code \| cancel]` | Über das offizielle `agy` anmelden, Google-Konto wechseln oder eine SSH-Code-Anmeldung abschließen und diese Sitzung einschalten |
| `/neoswitch:on` | Diese Sitzung ab der nächsten Nachricht auf Gemini 4 Neon umschalten |
| `/neoswitch:off` | Diese Sitzung ab der nächsten Nachricht zurück auf Claude schalten |
| `/neoswitch:status [-v]` | Aktives Modell, Google-Anmeldung und 5-Stunden-/Wochen-Kontingentbalken anzeigen (`-v` ergänzt alle Kontingentgruppen, Credits, Backend und Daemon-Details) |

## Vergleich mit anderen Ansätzen

| | **NeoSwitch** | **Konfigurationsmanager & Router**<br>(z. B. CC Switch, Claude Code Router) | **Eigenständige Antigravity-Proxies**<br>(z. B. antigravity-claude-proxy) |
|---|---|---|---|
| **Einrichtung** | Natives Claude-Code-Plugin — keine Desktop-App und kein separater Server nötig | Eigene Desktop-App zur Verwaltung mehrerer CLIs | Eigenständiger Node.js-Proxy-Server + Web-UI |
| **Wirkungsbereich** | **Eine Sitzung auf einmal** (`/neoswitch:on`, `/neoswitch:off`); andere Fenster bleiben bei Claude | Globaler Anbieterwechsel oder gemeinsamer lokaler Router | Globale `~/.claude/settings.json` oder Shell-Umgebungsvariablen |
| **Claude-Code-Funktionen** | **Bleiben erhalten** — kein `ANTHROPIC_AUTH_TOKEN`, sodass `claude.ai`-Konnektoren, `WebSearch`, Auto-Modus, Live-Kontingent und native Mod-Befehle weiter funktionieren | Verwaltung über externe Konfiguration oder Router | Setzt `ANTHROPIC_AUTH_TOKEN`, wodurch `claude.ai`-Konnektoren deaktiviert werden |
| **Google-Anmeldung** | Nutzt Ihr Google-Konto über das offizielle `agy` — **liest, speichert und wiederholt keine OAuth-Tokens**; eigene `agy`-Tools werden blockiert | Vorwiegend API-Schlüssel und Drittanbieter-Relays (keine `agy`-Integration) | Speichert oder liest lokale OAuth-Tokens für direkte Aufrufe privater Endpunkte |
| **Am besten für** | Normale Claude-Code-Nutzung mit gezieltem Wechsel einzelner Sitzungen auf Gemini 4 Neon | Zentrale Verwaltung vieler CLIs, API-Schlüssel, Relays und MCPs | Multi-Account-Rotation, Windows oder Claude-Modelle über Antigravity |

## Gut zu wissen

- **Modellbeschriftung:** Die obere Kopfzeile von Claude Code zeigt weiterhin den Claude-Modellnamen (wie `claude-opus-5-5`), und `/model` ändert das Backend-Modell nicht. Ab Claude Code 2.1.326 zeigt eine Statuszeile unter dem Prompt `Gemini 4 Neon answers this session`, solange NeoSwitch aktiv ist.
- **Latenz, `/effort` und Thinking:** Eine Antwort kann 5 bis 40 Sekunden brauchen, bis die ersten Wörter erscheinen. `/effort low` lässt Antworten früher beginnen (sowohl bei Antigravity als auch bei Gemini API / Vertex AI), und `NEOSWITCH_SHOW_THINKING=on` zeigt das Upstream-Reasoning als Claude-Code-Thinking-Blöcke.
- **Erster Lauf nach Installation oder Update:** Der erste NeoSwitch-Befehl nach einer Installation oder einem Update kann bis zu einer Minute dauern, während NeoSwitch das geprüfte Release-Binary baut oder herunterlädt.
- **Auto-Modus:** Funktioniert direkt mit NeoSwitchs Fail-Closed-Sicherheitsklassifikator (`gemini-3.8-flash` unter Antigravity, kein Claude-Verbrauch, kein Neustart). Setzen Sie `NEOSWITCH_SAFEGUARD=off`, um ihn abzuschalten, oder starten Sie Claude Code mit `CLAUDE_CODE_AUTO_MODE_SERVER=0 claude`, damit stattdessen Claude jede Aktion prüft (verbraucht etwas Claude-Kontingent, Fallback auf Gemini falls Claude nicht antwortet).
- **Täglicher Hinweis:** Sobald NeoSwitch eingerichtet ist, öffnet eine neue Sitzung ohne NeoSwitch höchstens einmal täglich pro Projekt mit einem einzeiligen `/neoswitch:on`-Hinweis (entfällt in Projekten, in denen Sie `/neoswitch:off` ausgeführt haben; `NEOSWITCH_NO_HINT=1` schaltet ihn ab).
- **Automatische Updates:** Wenn ein neues Release verfügbar ist, weisen Sitzungshinweis und `/neoswitch:status` darauf hin. Bei aktiviertem auto-update wechseln offene Claude-Code-Fenster automatisch auf den neuen Daemon, sobald eine neue Sitzung startet.

## Wenn etwas nicht klappt

<details>
<summary>Häufige Probleme und Lösungen (zum Aufklappen klicken)</summary>

| Was Sie sehen | Was zu tun ist |
|---|---|
| Der Sitzungshinweis sagt, NeoSwitch sei noch nicht eingerichtet | Führen Sie `/neoswitch:login` aus. Ihre nächste Nachricht nutzt Gemini 4 Neon. |
| Eine Meldung sagt, `agy` sei nicht installiert | `/neoswitch:login` konnte es nicht selbst installieren. Führen Sie `curl -fsSL https://antigravity.google/cli/install.sh \| bash` in einem Terminal aus oder installieren Sie es über [Googles Download-Seite](https://antigravity.google/download), und führen Sie dann `/neoswitch:login` erneut aus. |
| Jede Nachricht scheitert mit einem Verbindungsfehler, oder der Sitzungshinweis sagt, diese Sitzung könne nicht antworten | Führen Sie `/neoswitch:off` aus: ab Claude Code 2.1.326 schaltet dies die Sitzung lokal ohne Modellaufruf auf Claude zurück. Läuft der Daemon selbst nicht oder scheitern Nachrichten weiter, geben Sie `/exit` ein und starten Sie `claude` erneut: Eine neue Sitzung nutzt Claude, und `/neoswitch:status` sagt Ihnen, was zu beheben ist. |
| Der Hinweis sagt, andere Claude-Code-Fenster teilten noch die bisherige Einrichtung | Führen Sie `/neoswitch:on` in dieser Sitzung aus: offene Fenster wechseln automatisch auf die aktuelle Version. |
| Eine Warnung sagt, `claude.ai`-Konnektoren seien deaktiviert, weil eine andere Anmeldequelle gesetzt ist | NeoSwitch verursacht das nicht mehr; eine frühere Version tat es. Aktualisieren Sie das Plugin (siehe unten). Bleibt die Warnung, setzt ein anderes Werkzeug `ANTHROPIC_AUTH_TOKEN` oder `ANTHROPIC_API_KEY` für Claude Code. |
| Der Hinweis sagt, ein anderes Programm habe den von NeoSwitch genutzten Port übernommen | NeoSwitch ist bereits auf einen freien Port gewechselt und hat das Projekt für die nächste Sitzung auf Claude zurückgesetzt. Starten Sie eine neue Sitzung (nutzt Claude) und führen Sie `/neoswitch:on` für Gemini aus. |
| Der Hinweis sagt, NeoSwitch habe sein Programm nicht herunterladen können | Prüfen Sie Ihre Internetverbindung und starten Sie Claude Code erneut. Wenn NeoSwitch auf diesem Computer schon lief, nutzt es die zwischengespeicherte Version weiter, bis der Download klappt. |
| `/neoswitch:off` meldet selbst, dass NeoSwitch nicht laufen konnte | Es nennt die Einträge, die Sie aus `.claude/settings.local.json` des Projekts löschen; löschen Sie sie und starten Sie eine neue Sitzung. Schritt 1 unter [NeoSwitch entfernen](#ausschalten-aktualisieren-oder-entfernen) nennt dieselben Einträge. |
| Claude Code verbindet sich in einem Projekt nicht mehr, nachdem NeoSwitch entfernt wurde | Das Projekt zeigt noch auf NeoSwitch. Löschen Sie seine Einträge aus `.claude/settings.local.json`, wie Schritt 1 unter [NeoSwitch entfernen](#ausschalten-aktualisieren-oder-entfernen) beschreibt, und starten Sie eine neue Sitzung. |
| `/neoswitch:login` ist ein unbekannter Befehl | Aktualisieren Sie das Plugin (siehe unten), starten Sie Claude Code neu und führen Sie `/reload-plugins` aus. |
| Eine Meldung sagt, der Antigravity-Tarif Ihres Google-Kontos biete `gemini-4-neon` nicht an | NeoSwitch hat diese Sitzung bei Claude belassen und die verfügbaren Modelle Ihres Tarifs aufgelistet. Führen Sie `/neoswitch:login` für ein Konto mit `gemini-4-neon` aus, oder exportieren Sie `GEMINI_MODEL` mit einem der aufgelisteten Modelle. |

</details>

## Ausschalten, aktualisieren oder entfernen

- `/neoswitch:off` schaltet diese Sitzung ab ihrer nächsten Nachricht zurück auf Claude, mit Ihrer eigenen Claude-Anmeldung. `/neoswitch:on` schaltet sie wieder auf Gemini 4 Neon, ohne dass Sie etwas neu einrichten. Mit dem Schließen einer Sitzung endet ihre Umschaltung: Jede neue Sitzung beginnt mit Claude.

Um von Hand zu aktualisieren, führen Sie dies in einem Terminal aus und öffnen Sie dann eine neue Claude-Code-Sitzung (geöffnete Fenster wechseln automatisch zur neuen Version):

```bash
claude plugin marketplace update neoswitch
claude plugin update neoswitch@neoswitch
```

Wenn Sie den Marketplace unter seinem alten Namen `yldm-ai/NeoSwitch` / `yldm-ai/neon-switch` hinzugefügt haben, fügen Sie ihn zuerst mit `claude plugin marketplace add yldm-ai/neo-switch` erneut hinzu.

So entfernen Sie NeoSwitch:

1. Führen Sie zuerst in jedem Projekt, das Sie umgeschaltet haben, `/neoswitch:off` aus. Ein eingeschaltet gelassenes Projekt zeigt weiter auf NeoSwitch und antwortet nicht mehr, sobald NeoSwitch entfernt ist. Wenn Sie das Plugin schon entfernt haben, öffnen Sie in jedem solchen Projekt `.claude/settings.local.json` und ändern Sie das `env`-Objekt: Löschen Sie `ANTHROPIC_BASE_URL`, wenn es mit `https://127.0.0.1:` beginnt (bei früheren Versionen `http://127.0.0.1:`, zusammen mit dem `ANTHROPIC_AUTH_TOKEN` daneben); löschen Sie die Zeile `X-NeoSwitch-Token` in `ANTHROPIC_CUSTOM_HEADERS` und den ganzen Eintrag, wenn sonst nichts übrig bleibt; löschen Sie `NODE_EXTRA_CA_CERTS`, wenn es `tls/bundle.pem` im Ordner von NeoSwitch nennt, oder setzen Sie Ihre eigene CA-Datei wieder ein, falls Sie dort eine hatten; und löschen Sie `CLAUDE_CODE_GATEWAY_HINT_HEADERS` mit dem Wert `1` und `CLAUDE_CODE_USE_GATEWAY` mit dem Wert `0`, sofern Sie sie nicht selbst gesetzt haben.
2. Schließen Sie die Claude-Code-Fenster, die NeoSwitch nutzen, und führen Sie dies in einem Terminal aus:

```bash
claude plugin uninstall neoswitch@neoswitch
claude plugin marketplace remove neoswitch
```

3. Löschen Sie die Dateien, die NeoSwitch außerhalb Ihrer Projekte aufbewahrt:

| Was | Wo |
|---|---|
| Das NeoSwitch-Programm, das das Plugin herunterlädt oder baut | `$XDG_CACHE_HOME/neoswitch/`, oder `~/.cache/neoswitch/`, wenn `XDG_CACHE_HOME` nicht gesetzt ist |
| Status, TLS-Bundle, isolierter `agy`-Workspace und `daemon.log` | `NEOSWITCH_STATE_DIR`, wenn gesetzt; sonst `~/Library/Application Support/neoswitch/` unter macOS und `$XDG_CONFIG_HOME/neoswitch/` oder `~/.config/neoswitch/` unter Linux |

Die Google-Anmeldung speichert `agy`, nicht NeoSwitch, und sie bleibt bestehen; ebenso `agy` selbst, auch wenn `/neoswitch:login` es installiert hat.

## Was NeoSwitch ändert

Solange eine Sitzung in einem Projekt Gemini 4 Neon nutzt, fügt NeoSwitch dem `env`-Objekt der `.claude/settings.local.json` dieses Projekts vier Einträge hinzu und entfernt sie, sobald keine Sitzung dort es mehr nutzt:

- `ANTHROPIC_BASE_URL`, `ANTHROPIC_CUSTOM_HEADERS` und `NODE_EXTRA_CA_CERTS` schicken die Claude-Code-Anfragen des Projekts über eine verschlüsselte Verbindung, die nur NeoSwitch beantworten kann, an NeoSwitch auf diesem Computer. NeoSwitch beantwortet die umgeschalteten Sitzungen mit Gemini 4 Neon und reicht alle anderen Anfragen unverändert an Claude weiter, mit Ihrer eigenen Claude-Anmeldung, die es weder speichert noch protokolliert. Der Header enthält einen Schlüssel, der nur NeoSwitch auf diesem Computer öffnet. Hatte die Datei ein eigenes `NODE_EXTRA_CA_CERTS` oder `ANTHROPIC_CUSTOM_HEADERS`, bewahrt NeoSwitch eine Kopie auf und setzt Ihren Wert wieder ein, wenn es seine Einträge entfernt.
- `CLAUDE_CODE_GATEWAY_HINT_HEADERS=1` lässt Claude Code im Header angeben, ob eine Anfrage aus Ihrem Gespräch, einem Subagenten, einer Komprimierung oder einer Hintergrundaufgabe wie einem Prompt-Vorschlag stammt. NeoSwitch braucht dies, um die Gemini-Sitzung Ihres Gesprächs für dieses Gespräch zu reservieren. Haben Sie diesen Eintrag selbst gesetzt, lässt NeoSwitch Ihren Wert unangetastet.

In einem Git-Projekt trägt NeoSwitch `.claude/settings.local.json` außerdem in die klon-eigene Ausschlussliste (`.git/info/exclude`) ein, damit das Token nicht in Commits landet; Ihre `.gitignore` bleibt unverändert.

NeoSwitch läuft nur auf diesem Computer und nimmt nur Verbindungen von ihm an. Es protokolliert keine Prompts, Antworten, Schlüssel oder Anmeldedaten, und kein Server von uns ist beteiligt. Die Google-Anmeldung bleibt in `agy`; NeoSwitch liest sie nie.

## Mehr

Was funktioniert und was nicht, wie NeoSwitch funktioniert, das eigenständige Kommandozeilenwerkzeug, Umgebungsvariablen, Roadmap und Entwicklung: [docs/cli.md](../docs/cli.md) (auf Englisch). Außerdem: [Details zur Antigravity-Anmeldung](../docs/antigravity.md) und der [Protokollvertrag](../docs/protocol-contract.md).

Siehe [CONTRIBUTING.md](../CONTRIBUTING.md) zum Mitwirken, [SECURITY.md](../SECURITY.md) zum vertraulichen Melden einer Sicherheitslücke und [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md) für den Verhaltenskodex.

MIT License
