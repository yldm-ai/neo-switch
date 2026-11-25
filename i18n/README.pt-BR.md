# NeoSwitch

[English](../README.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Español](README.es.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | **Português (Brasil)** | [Русский](README.ru.md)

[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-D97757)](https://github.com/yldm-ai/neo-switch/blob/main/.claude-plugin/plugin.json) [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/yldm-ai/neo-switch/blob/main/LICENSE)

O NeoSwitch faz o Claude Code responder com o modelo Gemini 4 Neon do Google em vez do Claude, enquanto todo o resto do Claude Code continua igual.

[![Configurar o NeoSwitch em três passos](../docs/media/neoswitch-demo.gif)](../docs/media/neoswitch-demo.mp4)

Você troca um projeto por vez; seus outros projetos continuam usando o Claude. O plugin funciona no macOS e no Linux. O Windows ainda não é compatível.

## Como o NeoSwitch se compara

| | **NeoSwitch** | **Gerenciadores de config e roteadores**<br>(ex.: CC Switch, Claude Code Router) | **Proxies independentes do Antigravity**<br>(ex.: antigravity-claude-proxy) |
|---|---|---|---|
| **Instalação** | Plugin nativo do Claude Code — sem app de desktop nem servidor para gerenciar | App de desktop separado gerenciando várias CLIs | Servidor proxy Node.js independente + Web UI |
| **Escopo** | **Uma sessão por vez** (`/neoswitch:on`, `/neoswitch:off`); outras janelas continuam no Claude | Troca global de provedor ou roteador local compartilhado | `~/.claude/settings.json` global ou variáveis de ambiente |
| **Recursos do Claude Code** | **Preservados** — não define `ANTHROPIC_AUTH_TOKEN`, mantendo conectores do claude.ai e checagens do auto mode funcionando | Gerenciados via configuração externa ou roteador | Define `ANTHROPIC_AUTH_TOKEN`, o que desativa os conectores do claude.ai |
| **Login Google** | Usa sua conta Google pelo `agy` oficial — **nunca lê, armazena ou reutiliza tokens OAuth**, e bloqueia as ferramentas do próprio `agy` | Principalmente chaves de API e relays de terceiros (sem integração com `agy`) | Armazena ou lê tokens OAuth locais para chamar endpoints privados diretamente |
| **Ideal para** | Manter seu Claude Code normal e alternar sessões pontuais para o Gemini 4 Neon | Gerenciar várias CLIs, chaves de API, relays e MCPs em um só lugar | Rotação de múltiplas contas, Windows ou rodar modelos Claude via Antigravity |

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

O comando também mostra um link, para quando você não puder usar o navegador que ele abre, como em um computador que você acessa por SSH: abra o link em um navegador em qualquer dispositivo e faça login. No Claude Code 2.1.326 ou mais recente, o link aparece na hora e uma caixa de diálogo pede o código que a página mostra: cole-o ali. Em um Claude Code mais antigo, e no WSL ou no aplicativo de desktop por SSH, a resposta do Claude mostra o link: execute `/neoswitch:login` seguido do código. Execute `/neoswitch:login` de novo já conectado para trocar de conta Google: a atual continua valendo até você terminar, e `/neoswitch:login cancel` a mantém.

> [!WARNING]
> Os [Termos de Serviço do Google Antigravity](https://antigravity.google/terms) (seção 6) proíbem usar o Antigravity por meio de outras ferramentas, e o NeoSwitch é uma delas. O Google pode suspender ou encerrar as contas do Antigravity de quem fizer isso. `/neoswitch:login` repete este aviso toda vez. Não entre se você não aceita esse risco.

**3. Envie sua próxima mensagem.** A janela onde você executou o comando usa o Gemini 4 Neon a partir da próxima mensagem. As outras janelas e as novas sessões continuam no Claude, com seu próprio login do Claude e seus conectores: execute `/neoswitch:on` em cada sessão que deve responder com o Gemini 4 Neon. Se a resposta do comando pedir que você reinicie, porque o Claude Code foi iniciado por um gateway, inicie `claude` de novo sem ele no mesmo projeto.

## O que você vai ver

- Depois que o NeoSwitch está configurado, uma sessão nova que não o usa começa, no máximo uma vez por dia por projeto, com o aviso `NeoSwitch is set up: /neoswitch:on answers this session with Gemini 4 Neon, and /neoswitch:off returns it to Claude any time.` Ele não aparece em projetos onde você executou `/neoswitch:off`; `NEOSWITCH_NO_HINT=1` no seu ambiente o desativa.
- O cabeçalho continua mostrando um nome de modelo do Claude, como `claude-opus-5-5`. É só um rótulo: quem responde é o Gemini 4 Neon. Escolher outro modelo com `/model` não muda isso.
- Uma resposta pode levar de 5 a 40 segundos até as primeiras palavras aparecerem. Isso é normal. Se você fez login com `/neoswitch:login`, `/effort low` faz as respostas começarem mais cedo.
- O primeiro comando do NeoSwitch depois de instalar ou atualizar pode levar até um minuto enquanto o NeoSwitch se prepara.
- `/neoswitch:status` informa a qualquer momento se esta sessão usa o Claude ou o Gemini 4 Neon, e o que fazer em seguida.
- O modo auto numa sessão que usa o Gemini 4 Neon precisa do Claude Code iniciado com `CLAUDE_CODE_AUTO_MODE_SERVER=0 claude`. A verificação de segurança habitual roda no servidor da Anthropic, que nunca vê o que o Gemini faz, então o Claude Code pararia para perguntar antes das ações; um aviso diz isso uma vez. Iniciado assim, o Claude Code faz o Claude verificar cada ação: o NeoSwitch envia essas verificações, com a conversa que elas julgam, ao Claude com o seu login do Claude, o que gasta parte do seu uso do Claude, e ao Gemini só quando o Claude não responde. Se o Claude Code disser então que o modo auto não tem verificação própria, o modo auto não pode ser usado com o Gemini na sua conta; escolha outro modo com Shift+Tab.
- Quando houver uma versão mais nova do NeoSwitch, a nota da sessão avisa e mostra os comandos de atualização. Com auto-update ativado, o Claude Code a instala sozinho, e as janelas já abertas passam para ela assim que uma nova sessão do Claude Code começa. No Windows, ela passa a valer depois que você fechar todas as janelas do Claude Code e abri-las de novo.

## Se algo der errado

| O que você vê | O que fazer |
|---|---|
| O aviso da sessão diz que o NeoSwitch ainda não está configurado | Execute `/neoswitch:login`. Sua próxima mensagem usa o Gemini 4 Neon. |
| Uma mensagem diz que o `agy` não está instalado | O `/neoswitch:login` não conseguiu instalá-lo sozinho. Execute `curl -fsSL https://antigravity.google/cli/install.sh \| bash` em um terminal ou instale-o pela [página de download do Google](https://antigravity.google/download) e execute `/neoswitch:login` de novo. |
| Toda mensagem falha com erro de conexão, ou o aviso da sessão diz que esta sessão não pode responder | Execute `/neoswitch:off`: esta sessão continua no Claude a partir da próxima mensagem, mesmo que a resposta ao próprio comando falhe. Se as mensagens continuarem falhando, digite `/exit` e inicie `claude` de novo: uma sessão nova usa o Claude, e `/neoswitch:status` diz o que corrigir. |
| O aviso diz que outras janelas do Claude Code ainda compartilham a configuração anterior | Execute `/neoswitch:on` nesta sessão: todas as janelas abertas passam para a configuração dela. No Windows, feche todas as janelas do Claude Code, abra o Claude Code de novo e execute `/neoswitch:on`. |
| Um aviso diz que os conectores do claude.ai estão desativados porque outra fonte de autenticação está definida | O NeoSwitch não causa mais isso; uma versão anterior causava. Atualize o plugin (veja abaixo). Se continuar, outra ferramenta define `ANTHROPIC_AUTH_TOKEN` ou `ANTHROPIC_API_KEY` para o Claude Code. |
| O aviso diz que outro programa está usando a porta de que o NeoSwitch precisa | Feche esse programa e inicie o Claude Code de novo, ou execute `/neoswitch:off`. |
| O aviso diz que o NeoSwitch não conseguiu baixar seu programa | Verifique sua conexão com a internet e inicie o Claude Code de novo. Se o NeoSwitch já rodou neste computador, ele continua usando essa versão anterior até o download funcionar. |
| O próprio `/neoswitch:off` diz que o NeoSwitch não pôde ser executado | Ele lista as entradas a apagar do `.claude/settings.local.json` do projeto; apague-as e abra uma nova sessão. O passo 1 de [remover o NeoSwitch](#desligar-atualizar-ou-remover) lista as mesmas entradas. |
| O Claude Code não se conecta em um projeto depois que o NeoSwitch foi removido | O projeto ainda aponta para o NeoSwitch. Apague as entradas dele de `.claude/settings.local.json`, como descreve o passo 1 de [remover o NeoSwitch](#desligar-atualizar-ou-remover), e abra uma nova sessão. |
| `/neoswitch:login` aparece como comando desconhecido | Atualize o plugin (veja abaixo), reinicie o Claude Code e execute `/reload-plugins`. |
| Uma mensagem diz que o plano do Antigravity da sua conta Google não oferece gemini-4-neon | Seu plano não inclui o Gemini 4 Neon, então o NeoSwitch não pode responder com ele e mantém o projeto no Claude. Execute `/neoswitch:login` de novo para entrar com uma conta cujo plano o inclua, ou execute `/neoswitch:off` e abra uma nova sessão para voltar ao Claude. |

## Desligar, atualizar ou remover

- `/neoswitch:off` volta esta sessão para o Claude a partir da próxima mensagem, com seu próprio login do Claude. `/neoswitch:on` a muda de novo para o Gemini 4 Neon sem configurar nada outra vez. Fechar uma sessão encerra a troca dela: toda sessão nova começa no Claude.

Para atualizar manualmente, execute isto em um terminal e depois abra uma nova sessão do Claude Code: as janelas já abertas também passam para a nova versão (no Windows, feche todas as janelas do Claude Code e abra-as de novo):

```bash
claude plugin marketplace update neoswitch
claude plugin update neoswitch@neoswitch
```

Se você adicionou o marketplace com o nome antigo `yldm-ai/NeoSwitch` / `yldm-ai/neon-switch`, adicione-o de novo primeiro com `claude plugin marketplace add yldm-ai/neo-switch`.

Para remover o NeoSwitch:

1. Execute `/neoswitch:off` primeiro em cada projeto que você ligou. Um projeto deixado ligado continua apontando para o NeoSwitch e para de responder quando o NeoSwitch não existe mais. Se você já removeu o plugin, abra `.claude/settings.local.json` em cada um desses projetos e altere o objeto `env`: apague `ANTHROPIC_BASE_URL` quando começar com `https://127.0.0.1:` (em versões anteriores `http://127.0.0.1:`, junto com o `ANTHROPIC_AUTH_TOKEN` ao lado); apague a linha `X-NeoSwitch-Token` de `ANTHROPIC_CUSTOM_HEADERS`, e a entrada inteira quando não sobrar mais nada; apague `NODE_EXTRA_CA_CERTS` quando apontar para `tls/bundle.pem` na pasta do NeoSwitch, ou coloque de volta seu próprio arquivo de CA se você tinha um; e apague `CLAUDE_CODE_GATEWAY_HINT_HEADERS` com valor `1` e `CLAUDE_CODE_USE_GATEWAY` com valor `0`, a menos que você mesmo os tenha definido.
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

Enquanto uma sessão de um projeto usa o Gemini 4 Neon, o NeoSwitch adiciona três entradas ao objeto `env` do `.claude/settings.local.json` desse projeto e as remove quando nenhuma sessão ali o usa:

- `ANTHROPIC_BASE_URL`, `ANTHROPIC_CUSTOM_HEADERS` e `NODE_EXTRA_CA_CERTS` enviam as solicitações do Claude Code do projeto ao NeoSwitch neste computador, por uma conexão criptografada que só o NeoSwitch pode responder. O NeoSwitch responde com o Gemini 4 Neon às sessões que você trocou e repassa qualquer outra solicitação ao Claude sem mudanças, com seu próprio login do Claude, que ele não guarda nem registra. O cabeçalho leva uma chave que só abre o NeoSwitch neste computador. Se o arquivo tinha um `NODE_EXTRA_CA_CERTS` ou `ANTHROPIC_CUSTOM_HEADERS` seu, o NeoSwitch guarda uma cópia e coloca seu valor de volta quando remove as entradas dele.

Em um projeto Git, o NeoSwitch também adiciona `.claude/settings.local.json` à lista de exclusão do próprio clone (`.git/info/exclude`), para o token ficar fora dos commits; seu `.gitignore` não é alterado.

O NeoSwitch roda só neste computador e só aceita conexões vindas dele. Ele não registra prompts, respostas, chaves nem dados de login, e nenhum servidor nosso participa. O login do Google fica dentro do `agy`; o NeoSwitch nunca o lê.

## Mais

O que funciona e o que não funciona, como o NeoSwitch funciona, a ferramenta de linha de comando independente, as variáveis de ambiente, o roadmap e o desenvolvimento: [docs/cli.md](../docs/cli.md) (em inglês). Veja também: [detalhes do login do Antigravity](../docs/antigravity.md) e o [contrato do protocolo](../docs/protocol-contract.md).

Veja [CONTRIBUTING.md](../CONTRIBUTING.md) para contribuir, [SECURITY.md](../SECURITY.md) para relatar uma vulnerabilidade de forma privada e [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md) para o código de conduta.

MIT License
