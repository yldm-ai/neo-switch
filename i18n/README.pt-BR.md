# NeoSwitch

[English](../README.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Español](README.es.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | **Português (Brasil)** | [Русский](README.ru.md)

[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-D97757)](https://github.com/yldm-ai/neo-switch/blob/main/.claude-plugin/plugin.json) [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/yldm-ai/neo-switch/blob/main/LICENSE)

O NeoSwitch faz o Claude Code responder com o modelo Gemini 4 Neon do Google em vez do Claude, enquanto todo o resto do Claude Code continua igual.

[![Configurar o NeoSwitch em três passos](../docs/media/neoswitch-demo.gif)](../docs/media/neoswitch-demo.mp4)

Você troca um projeto por vez; seus outros projetos continuam usando o Claude. O plugin funciona no macOS e no Linux. O Windows ainda não é compatível.

## Primeiros passos

**1. Instale o plugin.** No Claude Code, execute estas duas linhas, uma de cada vez. Copie cada linha inteira: a primeira diz `neo-switch` e a segunda `neoswitch`.

```text
/plugin marketplace add yldm-ai/neo-switch
```

```text
/plugin install neoswitch@neoswitch
```

Depois ative as atualizações automáticas, para receber as correções quando o Google ou o Claude Code mudarem algo de que o NeoSwitch depende: execute `/plugin`, abra a aba **Marketplaces**, escolha `neoswitch` e ative auto-update. O Claude Code deixa isso desativado nos marketplaces que não são da Anthropic.

**2. Entre com sua conta Google.** Abra o Claude Code no projeto que você quer trocar e execute:

```text
/neoswitch:login
```

Ele mostra o aviso sobre os termos do Google, abre o login do Google no navegador por meio da [ferramenta de linha de comando Antigravity do Google (`agy`)](https://antigravity.google/download) e depois troca o projeto. Se o `agy` ainda não estiver neste computador, ele o instala antes em `~/.local/bin` com o instalador oficial do Google (cerca de 230 MB; o instalador também adiciona essa pasta ao PATH do seu shell). Se você está à vontade com o terminal, uma chave de API do Gemini também funciona: veja [docs/cli.md](../docs/cli.md#gemini-api-key) (em inglês).

O comando também mostra um link, para quando você não puder usar o navegador que ele abre, como em um computador que você acessa por SSH: abra o link em um navegador em qualquer dispositivo, faça login e execute `/neoswitch:login` seguido do código que a página mostra. Execute `/neoswitch:login` de novo já conectado para trocar de conta Google: a atual continua valendo até você terminar, e `/neoswitch:login cancel` a mantém.

> [!WARNING]
> Os [Termos de Serviço do Google Antigravity](https://antigravity.google/terms) (seção 6) proíbem usar o Antigravity por meio de outras ferramentas, e o NeoSwitch é uma delas. O Google pode suspender ou encerrar as contas do Antigravity de quem fizer isso. `/neoswitch:login` repete este aviso toda vez. Não entre se você não aceita esse risco.

**3. Envie sua próxima mensagem.** A janela em que você executou o comando usa o Gemini 4 Neon a partir da próxima mensagem, assim como as sessões novas deste projeto. Se a resposta do comando pedir que você reinicie, porque o Claude Code foi iniciado por um gateway ou porque outras janelas do Claude Code ainda usam uma configuração anterior do NeoSwitch, feche as janelas indicadas e inicie `claude` de novo no mesmo projeto. Quando a última sessão que usa o NeoSwitch aqui é fechada, o projeto volta para o Claude e sua próxima sessão começa no Claude; execute `/neoswitch:on` para usar o Gemini 4 Neon de novo.

## O que você vai ver

- A nova sessão começa com um aviso como `NeoSwitch: this session uses Gemini 4 Neon through your Google account (Antigravity sign-in). Ignore the Claude model name in the header. The first reply can take a while.`
- O cabeçalho continua mostrando um nome de modelo do Claude, como `claude-opus-5-5`. É só um rótulo: quem responde é o Gemini 4 Neon. Escolher outro modelo com `/model` não muda isso.
- Uma resposta pode levar de 5 a 40 segundos até as primeiras palavras aparecerem. Isso é normal. Se você fez login com `/neoswitch:login`, `/effort low` faz as respostas começarem mais cedo.
- O primeiro comando do NeoSwitch depois de instalar ou atualizar pode levar até um minuto enquanto o NeoSwitch se prepara.
- `/neoswitch:status` informa a qualquer momento se esta sessão usa o Claude ou o Gemini 4 Neon, e o que fazer em seguida.
- Quando houver uma versão mais nova do NeoSwitch, a nota da sessão avisa e mostra os comandos de atualização. Com auto-update ativado, o Claude Code a instala sozinho, e ela passa a valer depois que você fechar todas as janelas do Claude Code e abri-las de novo.

## Se algo der errado

| O que você vê | O que fazer |
|---|---|
| O aviso da sessão diz que o NeoSwitch ainda não está configurado | Execute `/neoswitch:login`. Sua próxima mensagem usa o Gemini 4 Neon. |
| Uma mensagem diz que o `agy` não está instalado | O `/neoswitch:login` não conseguiu instalá-lo sozinho. Execute `curl -fsSL https://antigravity.google/cli/install.sh \| bash` em um terminal ou instale-o pela [página de download do Google](https://antigravity.google/download) e execute `/neoswitch:login` de novo. |
| Toda mensagem falha com um erro de conexão, ou o aviso da sessão diz que esta sessão não pode responder | Neste mesmo projeto, execute `/neoswitch:off`. Ele volta o projeto mesmo que a própria resposta também mostre um erro de conexão. Depois digite `/exit` e inicie `claude` de novo: você está de volta ao Claude, e `/neoswitch:status` diz o que corrigir. |
| O aviso diz que outras janelas do Claude Code ainda compartilham a configuração anterior | Feche todas as janelas do Claude Code, abra o Claude Code de novo e execute `/neoswitch:on`. |
| Um aviso diz que os conectores do claude.ai estão desativados porque outra fonte de autenticação está definida | É o esperado enquanto esta sessão usa o NeoSwitch: a chave de acesso local dele tem prioridade sobre o seu login do claude.ai. Os conectores voltam na sua próxima sessão quando o projeto volta para o Claude, ao fechar a última sessão que usa o NeoSwitch ou após `/neoswitch:off`. |
| O aviso diz que outro programa está usando a porta de que o NeoSwitch precisa | Feche esse programa e inicie o Claude Code de novo, ou execute `/neoswitch:off`. |
| O aviso diz que o NeoSwitch não conseguiu baixar seu programa | Verifique sua conexão com a internet e inicie o Claude Code de novo. |
| `/neoswitch:login` aparece como comando desconhecido | Atualize o plugin (veja abaixo), reinicie o Claude Code e execute `/reload-plugins`. |
| Uma mensagem diz que o plano do Antigravity da sua conta Google não oferece gemini-4-neon | Seu plano não inclui o Gemini 4 Neon, então o NeoSwitch não pode responder com ele e mantém o projeto no Claude. Execute `/neoswitch:login` de novo para entrar com uma conta cujo plano o inclua, ou execute `/neoswitch:off` e abra uma nova sessão para voltar ao Claude. |

## Desligar, atualizar ou remover

- `/neoswitch:off` volta este projeto para o Claude nas sessões novas; uma sessão que já usa o Gemini 4 Neon continua com ele até você digitar `/exit`. `/neoswitch:on` troca o projeto de novo para o Gemini 4 Neon sem configurar nada outra vez, a partir da próxima mensagem. Fechar a última sessão que usa o NeoSwitch em um projeto faz o mesmo.

Para atualizar manualmente, execute isto em um terminal e depois feche todas as janelas do Claude Code e abra-as de novo:

```bash
claude plugin marketplace update neoswitch
claude plugin update neoswitch@neoswitch
```

Se você adicionou o marketplace com o nome antigo `yldm-ai/NeoSwitch` / `yldm-ai/neon-switch`, adicione-o de novo primeiro com `claude plugin marketplace add yldm-ai/neo-switch`.

Para remover o NeoSwitch:

1. Execute `/neoswitch:off` primeiro em cada projeto que você ligou. Um projeto deixado ligado continua apontando para o NeoSwitch e para de responder quando o NeoSwitch não existe mais. Se você já removeu o plugin, abra `.claude/settings.local.json` em cada um desses projetos e apague `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN` e `CLAUDE_CODE_USE_GATEWAY` do objeto `env`.
2. Feche as janelas do Claude Code que usam o NeoSwitch e execute isto em um terminal:

```bash
claude plugin uninstall neoswitch@neoswitch
claude plugin marketplace remove neoswitch
```

3. Apague os arquivos que o NeoSwitch guarda fora dos seus projetos:

| O quê | Onde |
|---|---|
| O programa NeoSwitch que o plugin baixa ou compila | `$XDG_CACHE_HOME/neoswitch/`, ou `~/.cache/neoswitch/` quando `XDG_CACHE_HOME` não está definido |
| Configurações e log: `proxy.json` (endereço, chave de acesso e caminho escolhido) e `daemon.log` | `NEOSWITCH_STATE_DIR` quando definido; senão `~/Library/Application Support/neoswitch/` no macOS e `$XDG_CONFIG_HOME/neoswitch/` ou `~/.config/neoswitch/` no Linux |

O login do Google é guardado pelo `agy`, não pelo NeoSwitch, e continua onde está; o próprio `agy` também, mesmo que o `/neoswitch:login` o tenha instalado.

## O que o NeoSwitch altera

Em cada projeto que você liga, o NeoSwitch adiciona três entradas ao objeto `env` de `.claude/settings.local.json`, e `/neoswitch:off`, ou fechar a última sessão que usa o NeoSwitch ali, as remove de novo:

- `ANTHROPIC_BASE_URL` e `ANTHROPIC_AUTH_TOKEN` enviam as novas sessões do Claude Code do projeto para o NeoSwitch neste computador. O token só abre o NeoSwitch neste computador; não é uma credencial do Google nem da Anthropic.
- `CLAUDE_CODE_USE_GATEWAY=0` faz essas sessões falarem diretamente com o NeoSwitch. Algumas ferramentas que iniciam o Claude Code por outro gateway ligam o modo gateway, e então o Claude Code ignoraria o NeoSwitch.

Em um projeto Git, o NeoSwitch também adiciona `.claude/settings.local.json` à lista de exclusão do próprio clone (`.git/info/exclude`), para o token ficar fora dos commits; seu `.gitignore` não é alterado.

O NeoSwitch roda só neste computador e só aceita conexões vindas dele. Ele não registra prompts, respostas, chaves nem dados de login, e nenhum servidor nosso participa. O login do Google fica dentro do `agy`; o NeoSwitch nunca o lê.

## Mais

O que funciona e o que não funciona, como o NeoSwitch funciona, a ferramenta de linha de comando independente, as variáveis de ambiente, o roadmap e o desenvolvimento: [docs/cli.md](../docs/cli.md) (em inglês). Veja também: [detalhes do login do Antigravity](../docs/antigravity.md) e o [contrato do protocolo](../docs/protocol-contract.md).

Veja [CONTRIBUTING.md](../CONTRIBUTING.md) para contribuir, [SECURITY.md](../SECURITY.md) para relatar uma vulnerabilidade de forma privada e [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md) para o código de conduta.

MIT License
