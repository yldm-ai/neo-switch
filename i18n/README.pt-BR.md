# NeoSwitch

[English](../README.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Español](README.es.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | **Português (Brasil)** | [Русский](README.ru.md)

[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-D97757)](https://github.com/yldm-ai/neo-switch/blob/main/.claude-plugin/plugin.json) [![Release](https://img.shields.io/github/v/release/yldm-ai/neo-switch)](https://github.com/yldm-ai/neo-switch/releases/latest) [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/yldm-ai/neo-switch/blob/main/LICENSE)

O NeoSwitch permite alternar sessões individuais do Claude Code para o Gemini 4 Neon do Google, mantendo intactos sua configuração, conectores, permissões e demais janelas do Claude Code.

[![Configurar o NeoSwitch em três passos](../docs/media/neoswitch-demo.gif)](../docs/media/neoswitch-demo.mp4)

Alterne uma sessão por vez — outras janelas no mesmo projeto continuam usando o Claude. O plugin funciona no macOS e no Linux; o Windows ainda não é compatível.

## Destaques

- **Uma sessão por vez, volta na hora.** `/neoswitch:on` e `/neoswitch:off` afetam apenas a sessão atual a partir da próxima mensagem; outras janelas do mesmo projeto e novas sessões continuam no Claude, e fechar a última sessão alternada remove automaticamente as entradas do projeto.
- **O Claude Code continua sendo o Claude Code.** Nenhum `ANTHROPIC_AUTH_TOKEN` é gravado, então os conectores do `claude.ai` seguem funcionando; o `WebSearch` integrado busca no Google via Antigravity; o Auto Mode funciona de fábrica com um classificador de segurança fail-closed (sem reiniciar e sem consumo do Claude); `/effort` e blocos de pensamento opcionais (`NEOSWITCH_SHOW_THINKING`) são mapeados diretamente para o Gemini.
- **Comandos nativos instantâneos e cota ao vivo.** No Claude Code 2.1.326 ou mais recente, `/neoswitch:login`, `/neoswitch:on`, `/neoswitch:off` e `/neoswitch:status` respondem localmente sem turno de modelo, uma linha sob o prompt indica qual modelo responde, e `/neoswitch:status` mostra barras de cota de 5 horas e semanais do Antigravity com horários de reset sem gastar cota (`-v` inclui todos os grupos e créditos).
- **Feito para sessões longas reais.** A reutilização e fragmentação de sessões ACP evitam o corte de 192 KB do `agy`, um agente enxuto reduz a sobrecarga de prompt do `agy` de ~14k para menos de 1k tokens, chamadas paralelas de ferramentas trazem reparo de escapes JSON, e sugestões de prompt e títulos vão para o `gemini-3.8-flash` para poupar a cota do Gemini 4 Neon.
- **Local, isolado e reversível.** O tráfego fica em TLS local cuja chave privada da CA é descartada após a assinatura; o NeoSwitch nunca lê, armazena ou reutiliza tokens OAuth do Google; o `agy` roda em um workspace isolado com suas próprias ferramentas de arquivo e shell bloqueadas tanto pelo agente enxuto quanto por um hook `PreToolUse` verificado; seus próprios `NODE_EXTRA_CA_CERTS` e `ANTHROPIC_CUSTOM_HEADERS` são preservados e restaurados ao desligar.
- **Pronto para SSH e atualizações sem interrupção.** `/neoswitch:login` abre o navegador e exibe um fluxo de link + código para máquinas remotas/SSH, atualizações do plugin repassam a porta do daemon automaticamente sem fechar janelas abertas, e falhas de compilação ou download recorrem a uma versão em cache para que `/neoswitch:off` sempre tenha saída.
- **Vários backends.** Use sua conta Google pelo `agy` oficial, ou utilize `GEMINI_API_KEY`, Vertex AI ou o backend experimental OpenAI Responses.

## Início rápido

**1. Instale o plugin.** No Claude Code, execute estas duas linhas, uma de cada vez. Copie cada linha inteira: a primeira diz `neo-switch` e a segunda `neoswitch`.

```text
/plugin marketplace add yldm-ai/neo-switch
```

```text
/plugin install neoswitch@neoswitch
```

Ative as atualizações automáticas para receber correções quando o Google ou o Claude Code mudarem algo de que o NeoSwitch depende: execute `/plugin`, abra a aba **Marketplaces**, escolha `neoswitch` e ative auto-update. O Claude Code deixa isso desativado para marketplaces de terceiros.

**2. Entre com sua conta Google.** Abra o Claude Code no projeto que você quer alternar e execute:

```text
/neoswitch:login
```

Ele mostra o aviso sobre os termos do Google, instala a [CLI oficial do Antigravity (`agy`)](https://antigravity.google/download) em `~/.local/bin` (~230 MB) se ainda não existir, abre o login do Google no navegador e ativa esta sessão. Também exibe um link para máquinas remotas/SSH: abra em qualquer dispositivo, faça login e cole o código na caixa de diálogo do Claude Code 2.1.326+ (ou execute `/neoswitch:login <code>` em versões antigas, WSL ou no app de desktop via SSH). Execute `/neoswitch:login` de novo para trocar de conta Google, ou `/neoswitch:login cancel` para manter a atual. Prefere chave de API ou Vertex AI? Veja [docs/cli.md](../docs/cli.md#gemini-api-key) (em inglês).

> [!WARNING]
> Os [Termos de Serviço do Google Antigravity](https://antigravity.google/terms) (seção 6) proíbem usar o Antigravity por meio de outras ferramentas, e o NeoSwitch é uma delas. O Google pode suspender ou encerrar as contas do Antigravity de quem fizer isso. `/neoswitch:login` repete este aviso toda vez. Não entre se você não aceita esse risco.

**3. Envie sua próxima mensagem.** Esta sessão usa o Gemini 4 Neon a partir da próxima mensagem, enquanto outras janelas e novas sessões continuam no Claude. Se a resposta pedir que você reinicie porque o Claude Code foi iniciado por um gateway, inicie `claude` de novo sem ele no mesmo projeto.

| Comando | O que faz |
|---|---|
| `/neoswitch:login [code \| cancel]` | Entrar pelo `agy` oficial, trocar de conta Google ou concluir um login por código SSH e ativar esta sessão |
| `/neoswitch:on` | Alternar esta sessão para o Gemini 4 Neon a partir da próxima mensagem |
| `/neoswitch:off` | Voltar esta sessão para o Claude a partir da próxima mensagem |
| `/neoswitch:status [-v]` | Mostrar modelo ativo, login Google e barras de cota de 5 horas/semanais (`-v` inclui todos os grupos, créditos, backend e detalhes do daemon) |

## Como o NeoSwitch se compara

| | **NeoSwitch** | **Gerenciadores de config e roteadores**<br>(ex.: CC Switch, Claude Code Router) | **Proxies independentes do Antigravity**<br>(ex.: antigravity-claude-proxy) |
|---|---|---|---|
| **Instalação** | Plugin nativo do Claude Code — sem app de desktop nem servidor para gerenciar | App de desktop separado gerenciando várias CLIs | Servidor proxy Node.js independente + Web UI |
| **Escopo** | **Uma sessão por vez** (`/neoswitch:on`, `/neoswitch:off`); outras janelas continuam no Claude | Troca global de provedor ou roteador local compartilhado | `~/.claude/settings.json` global ou variáveis de ambiente |
| **Recursos do Claude Code** | **Preservados** — sem `ANTHROPIC_AUTH_TOKEN`, mantendo conectores do `claude.ai`, `WebSearch`, Auto Mode, cota ao vivo e comandos nativos funcionando | Gerenciados via configuração externa ou roteador | Define `ANTHROPIC_AUTH_TOKEN`, o que desativa os conectores do `claude.ai` |
| **Login Google** | Usa sua conta Google pelo `agy` oficial — **nunca lê, armazena ou reutiliza tokens OAuth**, e bloqueia as ferramentas do próprio `agy` | Principalmente chaves de API e relays de terceiros (sem integração com `agy`) | Armazena ou lê tokens OAuth locais para chamar endpoints privados diretamente |
| **Ideal para** | Manter seu Claude Code normal e alternar sessões pontuais para o Gemini 4 Neon | Gerenciar várias CLIs, chaves de API, relays e MCPs em um só lugar | Rotação de múltiplas contas, Windows ou rodar modelos Claude via Antigravity |

## Bom saber

- **Rótulo do modelo:** O cabeçalho superior do Claude Code continua mostrando o nome do modelo Claude (como `claude-opus-5-5`), e `/model` não altera o modelo do backend. No Claude Code 2.1.326 ou mais recente, uma linha de status sob o prompt mostra `Gemini 4 Neon answers this session` enquanto o NeoSwitch está ativo.
- **Latência, `/effort` e pensamento:** Uma resposta pode levar de 5 a 40 segundos até as primeiras palavras aparecerem. `/effort low` faz as respostas começarem mais cedo (no Antigravity e na API Gemini / Vertex AI), e `NEOSWITCH_SHOW_THINKING=on` exibe o raciocínio do modelo como blocos thinking no Claude Code.
- **Primeira execução após instalar ou atualizar:** O primeiro comando do NeoSwitch após instalar ou atualizar pode levar até um minuto enquanto o NeoSwitch compila ou baixa o binário verificado.
- **Auto Mode:** Funciona de fábrica com o classificador de segurança fail-closed do NeoSwitch (`gemini-3.8-flash` no Antigravity, zero uso do Claude, sem reiniciar). Defina `NEOSWITCH_SAFEGUARD=off` para desativar, ou inicie o Claude Code com `CLAUDE_CODE_AUTO_MODE_SERVER=0 claude` para verificar cada ação com seu login do Claude (consome algum uso do Claude, recorrendo ao Gemini se o Claude não responder).
- **Aviso diário:** Depois de configurado o NeoSwitch, uma sessão nova que não o usa abre no máximo uma vez por dia por projeto com um aviso de uma linha para `/neoswitch:on` (ignorado em projetos onde você executou `/neoswitch:off`; `NEOSWITCH_NO_HINT=1` desativa).
- **Atualizações automáticas:** Quando há uma nova versão, a nota da sessão e `/neoswitch:status` avisam. Com auto-update ativado, janelas abertas do Claude Code migram automaticamente para o daemon atualizado assim que uma nova sessão começa.

## Se algo der errado

<details>
<summary>Problemas comuns e soluções (clique para expandir)</summary>

| O que você vê | O que fazer |
|---|---|
| O aviso da sessão diz que o NeoSwitch ainda não está configurado | Execute `/neoswitch:login`. Sua próxima mensagem usa o Gemini 4 Neon. |
| Uma mensagem diz que o `agy` não está instalado | O `/neoswitch:login` não conseguiu instalá-lo sozinho. Execute `curl -fsSL https://antigravity.google/cli/install.sh \| bash` em um terminal ou instale-o pela [página de download do Google](https://antigravity.google/download) e execute `/neoswitch:login` de novo. |
| Toda mensagem falha com erro de conexão, ou o aviso da sessão diz que esta sessão não pode responder | Execute `/neoswitch:off`: no Claude Code 2.1.326+ ele volta esta sessão para o Claude localmente, sem chamada ao modelo. Se o próprio daemon não estiver rodando ou as mensagens continuarem falhando, digite `/exit` e inicie `claude` de novo: uma nova sessão usa o Claude e `/neoswitch:status` mostra o que corrigir. |
| O aviso diz que outras janelas do Claude Code ainda compartilham a configuração anterior | Execute `/neoswitch:on` nesta sessão: as janelas abertas passam automaticamente para a versão atual. |
| Um aviso diz que os conectores do `claude.ai` estão desativados porque outra fonte de autenticação está definida | O NeoSwitch não causa mais isso; uma versão anterior causava. Atualize o plugin (veja abaixo). Se continuar, outra ferramenta define `ANTHROPIC_AUTH_TOKEN` ou `ANTHROPIC_API_KEY` para o Claude Code. |
| O aviso diz que outro programa ocupou a porta que o NeoSwitch usava | O NeoSwitch já mudou para uma porta livre e devolveu o projeto ao Claude na próxima sessão. Abra uma nova sessão (que usa o Claude) e execute `/neoswitch:on` para usar o Gemini. |
| O aviso diz que o NeoSwitch não conseguiu baixar seu programa | Verifique sua conexão com a internet e inicie o Claude Code de novo. Se o NeoSwitch já rodou neste computador, ele continua usando a versão em cache até o download funcionar. |
| O próprio `/neoswitch:off` diz que o NeoSwitch não pôde ser executado | Ele lista as entradas a apagar do `.claude/settings.local.json` do projeto; apague-as e abra uma nova sessão. O passo 1 de [remover o NeoSwitch](#desligar-atualizar-ou-remover) lista as mesmas entradas. |
| O Claude Code não se conecta em um projeto depois que o NeoSwitch foi removido | O projeto ainda aponta para o NeoSwitch. Apague as entradas dele de `.claude/settings.local.json`, como descreve o passo 1 de [remover o NeoSwitch](#desligar-atualizar-ou-remover), e abra uma nova sessão. |
| `/neoswitch:login` aparece como comando desconhecido | Atualize o plugin (veja abaixo), reinicie o Claude Code e execute `/reload-plugins`. |
| Uma mensagem diz que o plano do Antigravity da sua conta Google não oferece `gemini-4-neon` | O NeoSwitch manteve esta sessão no Claude e listou os modelos disponíveis no seu plano. Execute `/neoswitch:login` com uma conta que inclua `gemini-4-neon`, ou exporte `GEMINI_MODEL` com um dos modelos listados. |

</details>

## Desligar, atualizar ou remover

- `/neoswitch:off` volta esta sessão para o Claude a partir da próxima mensagem, com seu próprio login do Claude. `/neoswitch:on` a muda de novo para o Gemini 4 Neon sem configurar nada outra vez. Fechar uma sessão encerra a troca dela: toda sessão nova começa no Claude.

Para atualizar manualmente, execute isto em um terminal e depois abra uma nova sessão do Claude Code (as janelas já abertas passam automaticamente para a nova versão):

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
| Estado, pacote TLS, workspace isolado do `agy` e `daemon.log` | `NEOSWITCH_STATE_DIR` quando definido; senão `~/Library/Application Support/neoswitch/` no macOS e `$XDG_CONFIG_HOME/neoswitch/` ou `~/.config/neoswitch/` no Linux |

O login do Google é guardado pelo `agy`, não pelo NeoSwitch, e continua onde está; o próprio `agy` também, mesmo que o `/neoswitch:login` o tenha instalado.

## O que o NeoSwitch altera

Enquanto uma sessão de um projeto usa o Gemini 4 Neon, o NeoSwitch adiciona quatro entradas ao objeto `env` do `.claude/settings.local.json` desse projeto e as remove quando nenhuma sessão ali o usa:

- `ANTHROPIC_BASE_URL`, `ANTHROPIC_CUSTOM_HEADERS` e `NODE_EXTRA_CA_CERTS` enviam as solicitações do Claude Code do projeto ao NeoSwitch neste computador, por uma conexão criptografada que só o NeoSwitch pode responder. O NeoSwitch responde com o Gemini 4 Neon às sessões que você trocou e repassa qualquer outra solicitação ao Claude sem mudanças, com seu próprio login do Claude, que ele não guarda nem registra. O cabeçalho leva uma chave que só abre o NeoSwitch neste computador. Se o arquivo tinha um `NODE_EXTRA_CA_CERTS` ou `ANTHROPIC_CUSTOM_HEADERS` seu, o NeoSwitch guarda uma cópia e coloca seu valor de volta quando remove as entradas dele.
- `CLAUDE_CODE_GATEWAY_HINT_HEADERS=1` faz o Claude Code informar em um cabeçalho se a solicitação vem da conversa principal, de um subagente, de uma compactação ou de uma tarefa em segundo plano como sugestão de prompt. O NeoSwitch precisa disso para manter a sessão Gemini dedicada à sua conversa. Se você mesmo definiu essa entrada, o NeoSwitch preserva o seu valor.

Em um projeto Git, o NeoSwitch também adiciona `.claude/settings.local.json` à lista de exclusão do próprio clone (`.git/info/exclude`), para o token ficar fora dos commits; seu `.gitignore` não é alterado.

O NeoSwitch roda só neste computador e só aceita conexões vindas dele. Ele não registra prompts, respostas, chaves nem dados de login, e nenhum servidor nosso participa. O login do Google fica dentro do `agy`; o NeoSwitch nunca o lê.

## Mais

O que funciona e o que não funciona, como o NeoSwitch funciona, a ferramenta de linha de comando independente, as variáveis de ambiente, o roadmap e o desenvolvimento: [docs/cli.md](../docs/cli.md) (em inglês). Veja também: [detalhes do login do Antigravity](../docs/antigravity.md) e o [contrato do protocolo](../docs/protocol-contract.md).

Veja [CONTRIBUTING.md](../CONTRIBUTING.md) para contribuir, [SECURITY.md](../SECURITY.md) para relatar uma vulnerabilidade de forma privada e [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md) para o código de conduta.

MIT License
