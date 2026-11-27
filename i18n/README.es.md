# NeoSwitch

[English](../README.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | **Español** | [Français](README.fr.md) | [Deutsch](README.de.md) | [Português (Brasil)](README.pt-BR.md) | [Русский](README.ru.md)

[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-D97757)](https://github.com/yldm-ai/neo-switch/blob/main/.claude-plugin/plugin.json) [![Release](https://img.shields.io/github/v/release/yldm-ai/neo-switch)](https://github.com/yldm-ai/neo-switch/releases/latest) [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/yldm-ai/neo-switch/blob/main/LICENSE)

NeoSwitch te permite cambiar sesiones concretas de Claude Code a Gemini 4 Neon de Google manteniendo intactos tu configuración, conectores, permisos y demás ventanas de Claude Code.

[![Configurar NeoSwitch en tres pasos](../docs/media/neoswitch-demo.gif)](../docs/media/neoswitch-demo.mp4)

Cambia una sesión a la vez — las demás ventanas del mismo proyecto siguen usando Claude. El plugin funciona en macOS y Linux; Windows todavía no es compatible.

## Puntos destacados

- **Una sesión a la vez, con vuelta en el acto.** `/neoswitch:on` y `/neoswitch:off` afectan solo a la sesión actual desde su siguiente mensaje; las demás ventanas del mismo proyecto y las sesiones nuevas siguen en Claude, y al cerrar la última sesión cambiada NeoSwitch retira sus entradas del proyecto automáticamente.
- **Claude Code sigue siendo Claude Code.** No se escribe `ANTHROPIC_AUTH_TOKEN`, así que los conectores de `claude.ai` siguen funcionando; `WebSearch` integrado busca en Google en Antigravity; el modo auto funciona de serie con un clasificador de seguridad fail-closed (sin reiniciar y sin uso de Claude); `/effort` y los bloques de razonamiento opcionales (`NEOSWITCH_SHOW_THINKING`) se traducen limpiamente a Gemini.
- **Comandos nativos al instante y cuota en vivo.** En Claude Code 2.1.326 o posterior, `/neoswitch:login`, `/neoswitch:on`, `/neoswitch:off` y `/neoswitch:status` responden al instante sin turno de modelo, una línea bajo el prompt muestra qué modelo responde y `/neoswitch:status` muestra barras de cuota de 5 horas y semanales de Antigravity con horas de reinicio sin gastar cuota (`-v` añade todos los grupos y créditos).
- **Pensado para sesiones largas reales.** La reutilización y fragmentación de sesiones ACP evita el truncado de 192 KB de `agy`, un agente reducido baja la sobrecarga de `agy` de ~14k a menos de 1k tokens, las llamadas paralelas a herramientas incluyen reparación de escapes JSON y las sugerencias y títulos se envían a `gemini-3.8-flash` para ahorrar cuota de Gemini 4 Neon.
- **Local, aislado y reversible.** El tráfico queda en TLS local cuya clave privada de CA se descarta tras firmar; NeoSwitch nunca lee, guarda ni reutiliza tokens OAuth de Google; `agy` corre en un espacio aislado con sus propias herramientas de archivo y shell bloqueadas tanto por el agente reducido como por un hook `PreToolUse` verificado; tus propios `NODE_EXTRA_CA_CERTS` y `ANTHROPIC_CUSTOM_HEADERS` se respaldan y restauran al apagar NeoSwitch.
- **Listo para SSH y actualizaciones sin cortes.** `/neoswitch:login` abre un navegador e imprime un enlace + código para máquinas remotas o por SSH, las actualizaciones del plugin traspasan el puerto del daemon automáticamente sin cerrar ventanas abiertas, y si falla una compilación o descarga se usa una versión en caché para que `/neoswitch:off` siempre tenga salida.
- **Varios backends.** Usa tu cuenta de Google a través del `agy` oficial, o aporta una `GEMINI_API_KEY`, Vertex AI o el backend experimental de OpenAI Responses.

## Inicio rápido

**1. Instala el plugin.** En Claude Code, ejecuta estas dos líneas una por una. Copia cada línea entera: la primera dice `neo-switch` y la segunda `neoswitch`.

```text
/plugin marketplace add yldm-ai/neo-switch
```

```text
/plugin install neoswitch@neoswitch
```

Activa las actualizaciones automáticas para recibir correcciones cuando Google o Claude Code cambien algo de lo que depende NeoSwitch: ejecuta `/plugin`, abre la pestaña **Marketplaces**, elige `neoswitch` y activa auto-update. Claude Code las deja desactivadas para marketplaces de terceros.

**2. Inicia sesión con tu cuenta de Google.** Abre Claude Code en el proyecto que quieres cambiar y ejecuta:

```text
/neoswitch:login
```

Muestra la advertencia sobre los términos de Google, instala la [CLI oficial de Antigravity (`agy`)](https://antigravity.google/download) en `~/.local/bin` (~230 MB) si falta, abre el inicio de sesión de Google en tu navegador y activa esta sesión. También imprime un enlace para máquinas remotas/SSH: ábrelo en cualquier dispositivo, inicia sesión y pega el código en el diálogo de Claude Code 2.1.326+ (o ejecuta `/neoswitch:login <code>` en versiones anteriores, WSL o la app de escritorio por SSH). Ejecuta `/neoswitch:login` de nuevo para cambiar de cuenta de Google, o `/neoswitch:login cancel` para conservar la actual. ¿Prefieres clave API o Vertex AI? Consulta [docs/cli.md](../docs/cli.md#gemini-api-key) (en inglés).

> [!WARNING]
> Los [Términos de Servicio de Google Antigravity](https://antigravity.google/terms) (sección 6) prohíben usar Antigravity a través de otras herramientas, y NeoSwitch es una de ellas. Google puede suspender o cerrar las cuentas de Antigravity de quienes lo hagan. `/neoswitch:login` repite esta advertencia cada vez. No inicies sesión si no aceptas ese riesgo.

**3. Envía tu siguiente mensaje.** Esta sesión usa Gemini 4 Neon desde su siguiente mensaje, mientras otras ventanas y sesiones nuevas siguen en Claude. Si la respuesta te pide reiniciar porque Claude Code se inició a través de una pasarela, vuelve a iniciar `claude` sin ella en el mismo proyecto.

| Comando | Qué hace |
|---|---|
| `/neoswitch:login [code \| cancel]` | Inicia sesión con el `agy` oficial, cambia de cuenta de Google o completa un inicio por código SSH y activa esta sesión |
| `/neoswitch:on` | Cambia esta sesión a Gemini 4 Neon desde su siguiente mensaje |
| `/neoswitch:off` | Devuelve esta sesión a Claude desde su siguiente mensaje |
| `/neoswitch:status [-v]` | Muestra el modelo activo, el estado de Google y barras de cuota de 5 horas/semanales (`-v` añade todos los grupos, créditos, backend y detalles del daemon) |

## Cómo se compara NeoSwitch

| | **NeoSwitch** | **Gestores de configuración y routers**<br>(p. ej., CC Switch, Claude Code Router) | **Proxies independientes de Antigravity**<br>(p. ej., antigravity-claude-proxy) |
|---|---|---|---|
| **Instalación** | Plugin nativo de Claude Code: sin aplicación de escritorio ni servidor que administrar | Aplicación de escritorio aparte para gestionar varias CLI | Servidor proxy independiente en Node.js + interfaz web |
| **Alcance** | **Una sesión a la vez** (`/neoswitch:on`, `/neoswitch:off`); las demás ventanas siguen en Claude | Cambio global de proveedor o router local compartido | `~/.claude/settings.json` global o variables de entorno |
| **Funciones de Claude Code** | **Intactas**: sin `ANTHROPIC_AUTH_TOKEN`, así que los conectores de `claude.ai`, `WebSearch`, el modo auto, la cuota en vivo y los comandos nativos siguen funcionando | Gestionado mediante configuración externa o router | Define `ANTHROPIC_AUTH_TOKEN`, lo que desactiva los conectores de `claude.ai` |
| **Inicio de sesión de Google** | Usa tu cuenta de Google a través del `agy` oficial: **nunca lee, guarda ni reutiliza tokens OAuth**, y bloquea las herramientas propias de `agy` | Principalmente claves API y relays de terceros (sin integración con `agy`) | Guarda o lee tokens OAuth locales para llamar directamente a endpoints privados |
| **Ideal para** | Mantener tu Claude Code habitual y cambiar sesiones concretas a Gemini 4 Neon | Gestionar muchas CLI, claves API, relays y MCP desde un solo lugar | Rotación de varias cuentas, Windows o usar modelos Claude a través de Antigravity |

## Conviene saber

- **Etiqueta del modelo:** La cabecera superior de Claude Code sigue mostrando el nombre del modelo de Claude (como `claude-opus-5-5`), y `/model` no cambia el modelo del backend. En Claude Code 2.1.326 o posterior, una línea bajo el prompt muestra `Gemini 4 Neon answers this session` mientras NeoSwitch está activo.
- **Latencia, `/effort` y razonamiento:** Una respuesta puede tardar de 5 a 40 segundos en mostrar sus primeras palabras. `/effort low` hace que las respuestas empiecen antes (tanto en Antigravity como en la API de Gemini / Vertex AI), y `NEOSWITCH_SHOW_THINKING=on` muestra el razonamiento del modelo como bloques de thinking en Claude Code.
- **Primera ejecución tras instalar o actualizar:** El primer comando de NeoSwitch tras instalar o actualizar puede tardar hasta un minuto mientras compila o descarga el binario verificado.
- **Modo auto:** Funciona de serie con el clasificador de seguridad fail-closed de NeoSwitch (`gemini-3.8-flash` en Antigravity, sin uso de Claude y sin reiniciar). Define `NEOSWITCH_SAFEGUARD=off` para desactivarlo, o inicia Claude Code con `CLAUDE_CODE_AUTO_MODE_SERVER=0 claude` para revisar cada acción con tu cuenta de Claude (consume algo de uso de Claude y recurre a Gemini si Claude no responde).
- **Aviso diario:** Una vez configurado NeoSwitch, una sesión nueva que no lo usa abre como mucho una vez al día por proyecto con un aviso de una línea de `/neoswitch:on` (se omite en proyectos donde ejecutaste `/neoswitch:off`; `NEOSWITCH_NO_HINT=1` lo desactiva).
- **Actualizaciones automáticas:** Cuando hay una versión nueva, la nota de sesión y `/neoswitch:status` avisan. Con auto-update activado, las ventanas abiertas de Claude Code pasan automáticamente al daemon actualizado en cuanto se inicia una sesión nueva.

## Si algo falla

<details>
<summary>Problemas comunes y soluciones (haz clic para desplegar)</summary>

| Lo que ves | Qué hacer |
|---|---|
| El aviso de la sesión dice que NeoSwitch aún no está configurado | Ejecuta `/neoswitch:login`. Tu siguiente mensaje usa Gemini 4 Neon. |
| Un mensaje dice que `agy` no está instalado | `/neoswitch:login` no pudo instalarlo por sí solo. Ejecuta `curl -fsSL https://antigravity.google/cli/install.sh \| bash` en una terminal o instálalo desde la [página de descarga de Google](https://antigravity.google/download), y vuelve a ejecutar `/neoswitch:login`. |
| Todos los mensajes fallan con un error de conexión, o el aviso de la sesión dice que esta sesión no puede responder | Ejecuta `/neoswitch:off`: en Claude Code 2.1.326+ devuelve esta sesión a Claude localmente sin llamada al modelo. Si el propio daemon no se está ejecutando o los mensajes siguen fallando, escribe `/exit` y vuelve a iniciar `claude`: una sesión nueva usa Claude y `/neoswitch:status` te indica qué arreglar. |
| El aviso dice que otras ventanas de Claude Code siguen compartiendo la configuración anterior | Ejecuta `/neoswitch:on` en esta sesión: las ventanas abiertas pasan a la versión actual automáticamente. |
| Un aviso dice que los conectores de `claude.ai` están desactivados porque hay otra fuente de autenticación | NeoSwitch ya no lo causa; lo causaba una versión anterior. Actualiza el plugin (ver abajo). Si sigue apareciendo, otra herramienta define `ANTHROPIC_AUTH_TOKEN` o `ANTHROPIC_API_KEY` para Claude Code. |
| El aviso dice que otro programa ocupó el puerto que usaba NeoSwitch | NeoSwitch ya se movió a un puerto libre y devolvió el proyecto a Claude para la siguiente sesión. Abre una sesión nueva (usará Claude) y ejecuta `/neoswitch:on` para usar Gemini. |
| El aviso dice que NeoSwitch no pudo descargar su programa | Comprueba tu conexión a internet y vuelve a iniciar Claude Code. Si NeoSwitch ya se ejecutó en este equipo, sigue usando esa versión en caché hasta que la descarga funcione. |
| El propio `/neoswitch:off` dice que NeoSwitch no pudo ejecutarse | Indica las entradas que hay que borrar del `.claude/settings.local.json` del proyecto; bórralas y abre una sesión nueva. El paso 1 de [quitar NeoSwitch](#desactivar-actualizar-o-quitar) indica las mismas entradas. |
| Claude Code no se conecta en un proyecto después de quitar NeoSwitch | El proyecto sigue apuntando a NeoSwitch. Borra sus entradas de `.claude/settings.local.json`, como indica el paso 1 de [quitar NeoSwitch](#desactivar-actualizar-o-quitar), y abre una sesión nueva. |
| `/neoswitch:login` aparece como comando desconocido | Actualiza el plugin (ver abajo), reinicia Claude Code y ejecuta `/reload-plugins`. |
| Un mensaje dice que el plan de Antigravity de tu cuenta de Google no ofrece `gemini-4-neon` | NeoSwitch dejó esta sesión en Claude y mostró los modelos que tu plan sí ofrece. Ejecuta `/neoswitch:login` para entrar con una cuenta que incluya `gemini-4-neon`, o exporta `GEMINI_MODEL` con uno de los modelos listados. |

</details>

## Desactivar, actualizar o quitar

- `/neoswitch:off` devuelve esta sesión a Claude desde su siguiente mensaje, con tu propio inicio de sesión de Claude. `/neoswitch:on` la vuelve a cambiar a Gemini 4 Neon sin configurar nada de nuevo. Cerrar una sesión termina su cambio: cada sesión nueva empieza en Claude.

Para actualizar a mano, ejecuta esto en una terminal y luego abre una sesión nueva de Claude Code (las ventanas ya abiertas pasan a la nueva versión automáticamente):

```bash
claude plugin marketplace update neoswitch
claude plugin update neoswitch@neoswitch
```

Si añadiste el marketplace con su nombre antiguo `yldm-ai/NeoSwitch` / `yldm-ai/neon-switch`, primero vuelve a añadirlo con `claude plugin marketplace add yldm-ai/neo-switch`.

Para quitar NeoSwitch:

1. Ejecuta primero `/neoswitch:off` en cada proyecto que activaste. Un proyecto que quede activado sigue apuntando a NeoSwitch y deja de responder cuando NeoSwitch ya no está. Si ya quitaste el plugin, abre `.claude/settings.local.json` en cada uno de esos proyectos y cambia su objeto `env`: borra `ANTHROPIC_BASE_URL` cuando empiece por `https://127.0.0.1:` (en versiones anteriores `http://127.0.0.1:`, junto con el `ANTHROPIC_AUTH_TOKEN` de al lado); borra la línea `X-NeoSwitch-Token` de `ANTHROPIC_CUSTOM_HEADERS`, y toda la entrada si no queda nada más; borra `NODE_EXTRA_CA_CERTS` cuando nombre `tls/bundle.pem` en la carpeta de NeoSwitch, o vuelve a poner tu propio archivo de CA si tenías uno; y borra `CLAUDE_CODE_GATEWAY_HINT_HEADERS` con valor `1` y `CLAUDE_CODE_USE_GATEWAY` con valor `0`, salvo que los pusieras tú.
2. Cierra las ventanas de Claude Code que usan NeoSwitch y ejecuta esto en una terminal:

```bash
claude plugin uninstall neoswitch@neoswitch
claude plugin marketplace remove neoswitch
```

3. Borra los archivos que NeoSwitch guarda fuera de tus proyectos:

| Qué | Dónde |
|---|---|
| El programa NeoSwitch que el plugin descarga o compila | `$XDG_CACHE_HOME/neoswitch/`, o `~/.cache/neoswitch/` si `XDG_CACHE_HOME` no está definido |
| Estado, paquete TLS, espacio aislado de `agy` y `daemon.log` | `NEOSWITCH_STATE_DIR` si está definido; si no, `~/Library/Application Support/neoswitch/` en macOS y `$XDG_CONFIG_HOME/neoswitch/` o `~/.config/neoswitch/` en Linux |

El inicio de sesión de Google lo guarda `agy`, no NeoSwitch, y se queda donde está; lo mismo `agy`, aunque lo haya instalado `/neoswitch:login`.

## Qué cambia NeoSwitch

Mientras una sesión de un proyecto usa Gemini 4 Neon, NeoSwitch añade cuatro entradas al objeto `env` del `.claude/settings.local.json` de ese proyecto, y las quita cuando ninguna sesión de allí lo usa:

- `ANTHROPIC_BASE_URL`, `ANTHROPIC_CUSTOM_HEADERS` y `NODE_EXTRA_CA_CERTS` envían las solicitudes de Claude Code del proyecto a NeoSwitch en este ordenador, por una conexión cifrada que solo NeoSwitch puede responder. NeoSwitch responde con Gemini 4 Neon a las sesiones que cambiaste y pasa cualquier otra solicitud a Claude sin cambios, con tu propio inicio de sesión de Claude, que no guarda ni registra. El encabezado lleva una clave que solo abre NeoSwitch en este ordenador. Si el archivo tenía un `NODE_EXTRA_CA_CERTS` o un `ANTHROPIC_CUSTOM_HEADERS` tuyo, NeoSwitch guarda una copia y vuelve a poner tu valor cuando quita sus entradas.
- `CLAUDE_CODE_GATEWAY_HINT_HEADERS=1` hace que Claude Code indique en un encabezado si una solicitud viene de tu conversación, de un subagente, de una compactación o de una tarea en segundo plano como una sugerencia de prompt. NeoSwitch lo necesita para reservar la sesión Gemini de tu conversación. Si ya definiste esta entrada tú mismo, NeoSwitch la deja intacta.

En un proyecto Git, NeoSwitch también añade `.claude/settings.local.json` a la lista de exclusión propia del clon (`.git/info/exclude`), para que el token no entre en los commits; tu `.gitignore` no se toca.

NeoSwitch se ejecuta solo en este ordenador y solo acepta conexiones desde él. No registra prompts, respuestas, claves ni datos de inicio de sesión, y no interviene ningún servidor nuestro. El inicio de sesión de Google se queda dentro de `agy`; NeoSwitch nunca lo lee.

## Más

Qué funciona y qué no, cómo funciona NeoSwitch, la herramienta de línea de comandos independiente, las variables de entorno, la hoja de ruta y el desarrollo: [docs/cli.md](../docs/cli.md) (en inglés). También: [detalles del inicio de sesión de Antigravity](../docs/antigravity.md) y el [contrato del protocolo](../docs/protocol-contract.md).

Consulta [CONTRIBUTING.md](../CONTRIBUTING.md) para contribuir, [SECURITY.md](../SECURITY.md) para informar de una vulnerabilidad en privado y [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md) para el código de conducta.

MIT License
