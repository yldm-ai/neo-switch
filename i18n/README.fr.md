# NeoSwitch

[English](../README.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Español](README.es.md) | **Français** | [Deutsch](README.de.md) | [Português (Brasil)](README.pt-BR.md) | [Русский](README.ru.md)

[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-D97757)](https://github.com/yldm-ai/neo-switch/blob/main/.claude-plugin/plugin.json) [![Release](https://img.shields.io/github/v/release/yldm-ai/neo-switch)](https://github.com/yldm-ai/neo-switch/releases/latest) [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/yldm-ai/neo-switch/blob/main/LICENSE)

NeoSwitch vous permet de basculer individuellement vos sessions Claude Code vers Gemini 4 Neon de Google tout en gardant votre configuration, vos connecteurs, vos permissions et vos autres fenêtres Claude Code inchangés.

[![Configurer NeoSwitch en trois étapes](../docs/media/neoswitch-demo.gif)](../docs/media/neoswitch-demo.mp4)

Basculez une session à la fois — les autres fenêtres du même projet continuent d'utiliser Claude. Le plugin fonctionne sous macOS et Linux ; Windows n'est pas encore pris en charge.

## Points forts

- **Une session à la fois, retour sur place.** `/neoswitch:on` et `/neoswitch:off` n'affectent que la session en cours dès son message suivant ; les autres fenêtres du même projet et les nouvelles sessions restent sur Claude, et fermer la dernière session basculée retire automatiquement les entrées du projet.
- **Claude Code reste Claude Code.** Aucun `ANTHROPIC_AUTH_TOKEN` n'est écrit, donc les connecteurs `claude.ai` continuent de fonctionner ; `WebSearch` intégré recherche sur Google via Antigravity ; le mode auto fonctionne immédiatement grâce à un classificateur de sécurité fail-closed (sans redémarrage, sans usage Claude) ; `/effort` et les blocs de réflexion optionnels (`NEOSWITCH_SHOW_THINKING`) sont traduits proprement vers Gemini.
- **Commandes natives instantanées et quota en direct.** Avec Claude Code 2.1.326 ou plus récent, `/neoswitch:login`, `/neoswitch:on`, `/neoswitch:off` et `/neoswitch:status` répondent directement sans tour de modèle, une ligne sous l'invite indique quel modèle répond, et `/neoswitch:status` affiche les barres de quota Antigravity sur 5 heures et hebdomadaires avec heures de réinitialisation sans dépenser de quota (`-v` ajoute tous les groupes et crédits).
- **Conçu pour de longues sessions réelles.** La réutilisation et le découpage des sessions ACP évitent la troncature à 192 Ko d'`agy`, un agent allégé ramène la surcharge d'`agy` d'environ 14k à moins de 1k tokens, les appels d'outils en parallèle réparent les échappements JSON, et les suggestions d'invite comme les titres de session passent par `gemini-3.8-flash` pour économiser le quota de Gemini 4 Neon.
- **Local, isolé, réversible.** Le trafic reste en TLS local dont la clé privée de CA est détruite après signature ; NeoSwitch ne lit, ne stocke et ne rejoue jamais les jetons OAuth de Google ; `agy` tourne dans un espace isolé dont les outils fichier et shell sont bloqués à la fois par l'agent allégé et par un hook `PreToolUse` vérifié ; vos propres `NODE_EXTRA_CA_CERTS` et `ANTHROPIC_CUSTOM_HEADERS` sont sauvegardés puis restaurés à l'arrêt.
- **Prêt pour SSH et mises à jour sans coupure.** `/neoswitch:login` ouvre un navigateur et affiche un lien + code pour les machines distantes/SSH, les mises à jour du plugin transfèrent automatiquement le port du démon sans fermer les fenêtres ouvertes, et tout échec de compilation ou de téléchargement se replie sur une version en cache afin que `/neoswitch:off` reste toujours disponible.
- **Plusieurs backends.** Utilisez votre compte Google via l'outil officiel `agy`, ou passez par `GEMINI_API_KEY`, Vertex AI ou le backend expérimental OpenAI Responses.

## Démarrage rapide

**1. Installez le plugin.** Dans Claude Code, exécutez ces deux lignes l'une après l'autre. Copiez chaque ligne en entier : la première contient `neo-switch`, la seconde `neoswitch`.

```text
/plugin marketplace add yldm-ai/neo-switch
```

```text
/plugin install neoswitch@neoswitch
```

Activez les mises à jour automatiques pour recevoir les correctifs quand Google ou Claude Code modifient quelque chose dont NeoSwitch dépend : exécutez `/plugin`, ouvrez l'onglet **Marketplaces**, choisissez `neoswitch` et activez auto-update. Claude Code les laisse désactivées pour les marketplaces tiers.

**2. Connectez-vous avec votre compte Google.** Ouvrez Claude Code dans le projet à basculer et exécutez :

```text
/neoswitch:login
```

La commande affiche l'avertissement sur les conditions de Google, installe la [CLI officielle Antigravity (`agy`)](https://antigravity.google/download) dans `~/.local/bin` (~230 Mo) si elle manque, ouvre la connexion Google dans votre navigateur et active cette session. Elle affiche aussi un lien pour les machines distantes/SSH : ouvrez-le sur n'importe quel appareil, connectez-vous et collez le code dans la boîte de dialogue de Claude Code 2.1.326+ (ou lancez `/neoswitch:login <code>` sur les versions plus anciennes, WSL ou l'application de bureau en SSH). Relancez `/neoswitch:login` pour changer de compte Google, ou `/neoswitch:login cancel` pour garder le compte actuel. Vous préférez une clé API ou Vertex AI ? Voir [docs/cli.md](../docs/cli.md#gemini-api-key) (en anglais).

> [!WARNING]
> Les [Conditions d'utilisation de Google Antigravity](https://antigravity.google/terms) (section 6) interdisent d'utiliser Antigravity par l'intermédiaire d'autres outils, et NeoSwitch en est un. Google peut suspendre ou fermer les comptes Antigravity des personnes qui le font. `/neoswitch:login` répète cet avertissement à chaque fois. Ne vous connectez pas si vous n'acceptez pas ce risque.

**3. Envoyez votre message suivant.** Cette session utilise Gemini 4 Neon dès son message suivant, tandis que les autres fenêtres et les nouvelles sessions restent sur Claude. Si la réponse vous demande de redémarrer parce que Claude Code a été lancé via une passerelle, relancez `claude` sans elle dans le même projet.

| Commande | Action |
|---|---|
| `/neoswitch:login [code \| cancel]` | Se connecter via `agy` officiel, changer de compte Google ou terminer une connexion par code SSH, puis activer cette session |
| `/neoswitch:on` | Basculer cette session vers Gemini 4 Neon dès son message suivant |
| `/neoswitch:off` | Ramener cette session sur Claude dès son message suivant |
| `/neoswitch:status [-v]` | Afficher le modèle actif, la connexion Google et les barres de quota sur 5 heures/hebdomadaires (`-v` ajoute tous les groupes, crédits, backend et détails du démon) |

## Comparaison avec les autres approches

| | **NeoSwitch** | **Gestionnaires de config et routeurs**<br>(ex. CC Switch, Claude Code Router) | **Proxies Antigravity autonomes**<br>(ex. antigravity-claude-proxy) |
|---|---|---|---|
| **Installation** | Plugin natif Claude Code — aucune application de bureau ni serveur à gérer | Application de bureau dédiée gérant plusieurs CLI | Serveur proxy Node.js autonome + interface web |
| **Portée** | **Une session à la fois** (`/neoswitch:on`, `/neoswitch:off`) ; les autres fenêtres restent sur Claude | Bascule globale de fournisseur ou routeur local partagé | `~/.claude/settings.json` global ou variables d'environnement |
| **Fonctions Claude Code** | **Préservées** — sans `ANTHROPIC_AUTH_TOKEN`, les connecteurs `claude.ai`, `WebSearch`, le mode auto, le quota en direct et les commandes natives continuent de fonctionner | Gérées via une configuration externe ou un routeur | Définit `ANTHROPIC_AUTH_TOKEN`, ce qui désactive les connecteurs `claude.ai` |
| **Connexion Google** | Passe par l'outil officiel `agy` — **ne lit, ne stocke et ne rejoue jamais vos jetons OAuth**, et bloque les outils internes d'`agy` | Principalement clés API et relais tiers (sans intégration `agy`) | Stocke ou lit les jetons OAuth locaux pour appeler directement des endpoints privés |
| **Idéal pour** | Garder votre Claude Code habituel et basculer certaines sessions vers Gemini 4 Neon | Gérer plusieurs CLI, clés API, relais et MCP au même endroit | Rotation multi-comptes, Windows ou utilisation de modèles Claude via Antigravity |

## Bon à savoir

- **Étiquette de modèle :** L'en-tête supérieur de Claude Code continue d'afficher le nom du modèle Claude (comme `claude-opus-5-5`), et `/model` ne change pas le modèle du backend. Avec Claude Code 2.1.326 ou plus récent, une ligne d'état sous l'invite affiche `Gemini 4 Neon answers this session` tant que NeoSwitch est actif.
- **Latence, `/effort` et réflexion :** Une réponse peut mettre 5 à 40 secondes avant d'afficher ses premiers mots. `/effort low` fait démarrer les réponses plus tôt (sur Antigravity comme sur l'API Gemini / Vertex AI), et `NEOSWITCH_SHOW_THINKING=on` affiche le raisonnement amont sous forme de blocs thinking dans Claude Code.
- **Première exécution après installation ou mise à jour :** La première commande NeoSwitch après une installation ou une mise à jour peut prendre jusqu'à une minute, le temps de compiler ou télécharger le binaire vérifié.
- **Mode auto :** Fonctionne immédiatement avec le classificateur de sécurité fail-closed de NeoSwitch (`gemini-3.8-flash` sur Antigravity, zéro usage Claude, sans redémarrage). Définissez `NEOSWITCH_SAFEGUARD=off` pour le désactiver, ou lancez Claude Code avec `CLAUDE_CODE_AUTO_MODE_SERVER=0 claude` pour faire vérifier les actions avec votre connexion Claude (consomme un peu d'usage Claude, avec repli sur Gemini si Claude ne répond pas).
- **Note quotidienne :** Une fois NeoSwitch configuré, une nouvelle session qui ne l'utilise pas s'ouvre au plus une fois par jour et par projet avec une note d'une ligne pour `/neoswitch:on` (ignorée dans les projets où vous avez lancé `/neoswitch:off` ; `NEOSWITCH_NO_HINT=1` la désactive).
- **Mises à jour automatiques :** Quand une nouvelle version sort, la note de session et `/neoswitch:status` le signalent. Avec auto-update activé, les fenêtres Claude Code déjà ouvertes basculent automatiquement sur le démon mis à jour dès le démarrage d'une nouvelle session.

## En cas de problème

<details>
<summary>Problèmes fréquents et solutions (cliquer pour déplier)</summary>

| Ce que vous voyez | Que faire |
|---|---|
| La note de session dit que NeoSwitch n'est pas encore configuré | Exécutez `/neoswitch:login`. Votre prochain message utilise Gemini 4 Neon. |
| Un message dit que `agy` n'est pas installé | `/neoswitch:login` n'a pas pu l'installer lui-même. Exécutez `curl -fsSL https://antigravity.google/cli/install.sh \| bash` dans un terminal ou installez-le depuis la [page de téléchargement de Google](https://antigravity.google/download), puis exécutez de nouveau `/neoswitch:login`. |
| Chaque message échoue avec une erreur de connexion, ou la note de session indique que cette session ne peut pas répondre | Lancez `/neoswitch:off` : avec Claude Code 2.1.326+, cette session revient localement à Claude sans appel de modèle. Si le démon lui-même ne tourne pas ou si les messages échouent encore, tapez `/exit` et relancez `claude` : une nouvelle session utilise Claude, et `/neoswitch:status` vous indique quoi corriger. |
| La note dit que d'autres fenêtres de Claude Code partagent encore la configuration précédente | Lancez `/neoswitch:on` dans cette session : les fenêtres ouvertes passent automatiquement à la version actuelle. |
| Un avertissement indique que les connecteurs `claude.ai` sont désactivés parce qu'une autre source d'authentification est définie | NeoSwitch ne le provoque plus ; une version antérieure le faisait. Mettez le plugin à jour (voir plus bas). S'il persiste, un autre outil définit `ANTHROPIC_AUTH_TOKEN` ou `ANTHROPIC_API_KEY` pour Claude Code. |
| La note dit qu'un autre programme a pris le port utilisé par NeoSwitch | NeoSwitch s'est déjà déplacé sur un port libre et a remis le projet sur Claude pour la prochaine session. Ouvrez une nouvelle session (qui utilise Claude) et lancez `/neoswitch:on` pour utiliser Gemini. |
| La note dit que NeoSwitch n'a pas pu télécharger son programme | Vérifiez votre connexion internet et relancez Claude Code. Si NeoSwitch a déjà fonctionné sur cet ordinateur, il continue d'utiliser la version en cache jusqu'à ce que le téléchargement réussisse. |
| `/neoswitch:off` lui-même indique que NeoSwitch n'a pas pu s'exécuter | Il liste les entrées à supprimer du `.claude/settings.local.json` du projet ; supprimez-les et ouvrez une nouvelle session. L'étape 1 de [supprimer NeoSwitch](#désactiver-mettre-à-jour-ou-supprimer) liste les mêmes entrées. |
| Claude Code ne se connecte plus dans un projet après la suppression de NeoSwitch | Le projet pointe toujours vers NeoSwitch. Supprimez ses entrées de `.claude/settings.local.json`, comme le décrit l'étape 1 de [supprimer NeoSwitch](#désactiver-mettre-à-jour-ou-supprimer), puis ouvrez une nouvelle session. |
| `/neoswitch:login` est une commande inconnue | Mettez le plugin à jour (voir ci-dessous), redémarrez Claude Code, puis exécutez `/reload-plugins`. |
| Un message indique que l'offre Antigravity de votre compte Google ne propose pas `gemini-4-neon` | NeoSwitch a laissé cette session sur Claude et listé les modèles inclus dans votre offre. Relancez `/neoswitch:login` avec un compte incluant `gemini-4-neon`, ou exportez `GEMINI_MODEL` sur l'un des modèles listés. |

</details>

## Désactiver, mettre à jour ou supprimer

- `/neoswitch:off` ramène cette session à Claude dès son message suivant, avec votre propre connexion Claude. `/neoswitch:on` la rebascule sur Gemini 4 Neon sans rien reconfigurer. Fermer une session met fin à sa bascule : chaque nouvelle session démarre sur Claude.

Pour mettre à jour à la main, exécutez ceci dans un terminal, puis ouvrez une nouvelle session Claude Code (les fenêtres déjà ouvertes passent automatiquement à la nouvelle version) :

```bash
claude plugin marketplace update neoswitch
claude plugin update neoswitch@neoswitch
```

Si vous avez ajouté le marketplace sous son ancien nom `yldm-ai/NeoSwitch` / `yldm-ai/neon-switch`, ajoutez-le d'abord de nouveau avec `claude plugin marketplace add yldm-ai/neo-switch`.

Pour supprimer NeoSwitch :

1. Exécutez d'abord `/neoswitch:off` dans chaque projet que vous avez basculé. Un projet laissé activé continue de pointer vers NeoSwitch et ne répond plus une fois NeoSwitch supprimé. Si vous avez déjà supprimé le plugin, ouvrez `.claude/settings.local.json` dans chacun de ces projets et modifiez son objet `env` : supprimez `ANTHROPIC_BASE_URL` s'il commence par `https://127.0.0.1:` (dans les versions antérieures `http://127.0.0.1:`, avec l'`ANTHROPIC_AUTH_TOKEN` voisin) ; supprimez la ligne `X-NeoSwitch-Token` de `ANTHROPIC_CUSTOM_HEADERS`, et toute l'entrée s'il ne reste rien d'autre ; supprimez `NODE_EXTRA_CA_CERTS` s'il désigne `tls/bundle.pem` dans le dossier de NeoSwitch, ou remettez votre propre fichier de CA si vous en aviez un ; et supprimez `CLAUDE_CODE_GATEWAY_HINT_HEADERS` à `1` et `CLAUDE_CODE_USE_GATEWAY` à `0`, sauf si vous les avez définis vous-même.
2. Fermez les fenêtres de Claude Code qui utilisent NeoSwitch, puis exécutez ceci dans un terminal :

```bash
claude plugin uninstall neoswitch@neoswitch
claude plugin marketplace remove neoswitch
```

3. Supprimez les fichiers que NeoSwitch garde en dehors de vos projets :

| Quoi | Où |
|---|---|
| Le programme NeoSwitch que le plugin télécharge ou compile | `$XDG_CACHE_HOME/neoswitch/`, ou `~/.cache/neoswitch/` si `XDG_CACHE_HOME` n'est pas défini |
| État, bundle TLS, espace `agy` isolé et `daemon.log` | `NEOSWITCH_STATE_DIR` s'il est défini ; sinon `~/Library/Application Support/neoswitch/` sous macOS et `$XDG_CONFIG_HOME/neoswitch/` ou `~/.config/neoswitch/` sous Linux |

La connexion Google est conservée par `agy`, pas par NeoSwitch, et reste en place ; `agy` lui-même aussi, même si `/neoswitch:login` l'a installé.

## Ce que NeoSwitch modifie

Tant qu'une session d'un projet utilise Gemini 4 Neon, NeoSwitch ajoute quatre entrées à l'objet `env` du `.claude/settings.local.json` de ce projet, et les retire dès qu'aucune session n'y utilise plus NeoSwitch :

- `ANTHROPIC_BASE_URL`, `ANTHROPIC_CUSTOM_HEADERS` et `NODE_EXTRA_CA_CERTS` envoient les requêtes Claude Code du projet à NeoSwitch sur cet ordinateur, par une connexion chiffrée à laquelle seul NeoSwitch peut répondre. NeoSwitch répond avec Gemini 4 Neon aux sessions que vous avez basculées et transmet toute autre requête à Claude sans la modifier, avec votre propre connexion Claude, qu'il ne conserve ni ne journalise. L'en-tête contient une clé qui n'ouvre que NeoSwitch sur cet ordinateur. Si le fichier contenait votre propre `NODE_EXTRA_CA_CERTS` ou `ANTHROPIC_CUSTOM_HEADERS`, NeoSwitch en garde une copie et remet votre valeur quand il retire ses entrées.
- `CLAUDE_CODE_GATEWAY_HINT_HEADERS=1` amène Claude Code à indiquer dans un en-tête si une requête vient de votre conversation, d'un sous-agent, d'une compaction ou d'une tâche d'arrière-plan comme une suggestion d'invite. NeoSwitch en a besoin pour garder la session Gemini de votre conversation. Si vous avez défini cette entrée vous-même, NeoSwitch ne touche pas à votre valeur.

Dans un projet Git, NeoSwitch ajoute aussi `.claude/settings.local.json` à la liste d'exclusion propre au clone (`.git/info/exclude`), pour que le jeton reste hors des commits ; votre `.gitignore` n'est pas modifié.

NeoSwitch ne tourne que sur cet ordinateur et n'accepte que les connexions qui en proviennent. Il n'enregistre ni prompts, ni réponses, ni clés, ni informations de connexion, et aucun serveur à nous n'intervient. La connexion Google reste dans `agy` ; NeoSwitch ne la lit jamais.

## Pour aller plus loin

Ce qui fonctionne et ce qui ne fonctionne pas, le fonctionnement de NeoSwitch, l'outil en ligne de commande autonome, les variables d'environnement, la feuille de route et le développement : [docs/cli.md](../docs/cli.md) (en anglais). Voir aussi : [détails de la connexion Antigravity](../docs/antigravity.md) et le [contrat du protocole](../docs/protocol-contract.md).

Consultez [CONTRIBUTING.md](../CONTRIBUTING.md) pour contribuer, [SECURITY.md](../SECURITY.md) pour signaler une vulnérabilité en privé et [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md) pour le code de conduite.

MIT License
