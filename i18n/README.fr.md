# NeoSwitch

[English](../README.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Español](README.es.md) | **Français** | [Deutsch](README.de.md) | [Português (Brasil)](README.pt-BR.md) | [Русский](README.ru.md)

[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-D97757)](https://github.com/yldm-ai/neo-switch/blob/main/.claude-plugin/plugin.json) [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/yldm-ai/neo-switch/blob/main/LICENSE)

NeoSwitch fait répondre Claude Code avec le modèle Gemini 4 Neon de Google au lieu de Claude, tout le reste de Claude Code restant identique.

[![Configurer NeoSwitch en trois étapes](../docs/media/neoswitch-demo.gif)](../docs/media/neoswitch-demo.mp4)

Vous basculez un projet à la fois ; vos autres projets continuent d'utiliser Claude. Le plugin fonctionne sous macOS et Linux. Windows n'est pas encore pris en charge.

## Démarrer

**1. Installez le plugin.** Dans Claude Code, exécutez ces deux lignes l'une après l'autre. Copiez chaque ligne en entier : la première contient `neo-switch`, la seconde `neoswitch`.

```text
/plugin marketplace add yldm-ai/neo-switch
```

```text
/plugin install neoswitch@neoswitch
```

Activez ensuite les mises à jour automatiques, pour recevoir les correctifs quand Google ou Claude Code modifient quelque chose dont NeoSwitch dépend : exécutez `/plugin`, ouvrez l'onglet **Marketplaces**, choisissez `neoswitch` et activez auto-update. Claude Code les laisse désactivées pour les marketplaces qui ne viennent pas d'Anthropic.

**2. Connectez-vous avec votre compte Google.** Ouvrez Claude Code dans le projet à basculer et exécutez :

```text
/neoswitch:login
```

La commande affiche l'avertissement sur les conditions de Google, ouvre la connexion Google dans votre navigateur via l'[outil en ligne de commande Antigravity de Google (`agy`)](https://antigravity.google/download), puis bascule le projet. Si `agy` n'est pas encore sur cet ordinateur, elle l'installe d'abord dans `~/.local/bin` avec l'installateur officiel de Google (environ 230 Mo ; l'installateur ajoute aussi ce dossier au PATH de votre shell). Si vous êtes à l'aise avec un terminal, une clé API Gemini fonctionne aussi : voir [docs/cli.md](../docs/cli.md#gemini-api-key) (en anglais).

La commande affiche aussi un lien, pour le cas où vous ne pouvez pas utiliser le navigateur qu'elle ouvre, par exemple sur une machine à laquelle vous accédez en SSH : ouvrez le lien dans un navigateur sur n'importe quel appareil et connectez-vous. Avec Claude Code 2.1.326 ou plus récent, le lien s'affiche aussitôt et une boîte de dialogue demande le code affiché par la page : collez-le à cet endroit. Avec un Claude Code plus ancien, ainsi que dans WSL ou l'application de bureau en SSH, c'est la réponse de Claude qui montre le lien : lancez alors `/neoswitch:login` suivi du code. Relancez `/neoswitch:login` une fois connecté pour passer à un autre compte Google : le compte actuel reste actif jusqu'à la fin, et `/neoswitch:login cancel` le conserve.

> [!WARNING]
> Les [Conditions d'utilisation de Google Antigravity](https://antigravity.google/terms) (section 6) interdisent d'utiliser Antigravity par l'intermédiaire d'autres outils, et NeoSwitch en est un. Google peut suspendre ou fermer les comptes Antigravity des personnes qui le font. `/neoswitch:login` répète cet avertissement à chaque fois. Ne vous connectez pas si vous n'acceptez pas ce risque.

**3. Envoyez votre message suivant.** La fenêtre où vous avez lancé la commande utilise Gemini 4 Neon dès son message suivant. Les autres fenêtres et les nouvelles sessions continuent avec Claude, avec votre propre connexion Claude et vos connecteurs : lancez `/neoswitch:on` dans chaque session qui doit répondre avec Gemini 4 Neon. Si la réponse de la commande vous demande de redémarrer, parce que Claude Code a été lancé via une passerelle, relancez `claude` sans elle dans le même projet.

## Ce que vous verrez

- Une fois NeoSwitch configuré, une nouvelle session qui ne l'utilise pas s'ouvre, au plus une fois par jour et par projet, avec la note `NeoSwitch is set up: /neoswitch:on answers this session with Gemini 4 Neon, and /neoswitch:off returns it to Claude any time.` Elle n'apparaît pas dans les projets où vous avez lancé `/neoswitch:off` ; `NEOSWITCH_NO_HINT=1` dans votre environnement la désactive.
- L'en-tête affiche toujours un nom de modèle Claude, comme `claude-opus-5-5`. Ce n'est qu'une étiquette : c'est Gemini 4 Neon qui répond. Choisir un autre modèle avec `/model` n'y change rien.
- Une réponse peut mettre 5 à 40 secondes avant d'afficher ses premiers mots. C'est normal. Si vous vous êtes connecté avec `/neoswitch:login`, `/effort low` fait démarrer les réponses plus tôt.
- La première commande NeoSwitch après une installation ou une mise à jour peut prendre jusqu'à une minute, le temps que NeoSwitch se prépare.
- `/neoswitch:status` vous indique à tout moment si cette session utilise Claude ou Gemini 4 Neon, et quoi faire ensuite.
- Le mode auto fonctionne immédiatement dans une session utilisant Gemini 4 Neon : NeoSwitch exécute son propre classificateur de sécurité fail-closed (via `gemini-3.8-flash` sur Antigravity, ou le modèle configuré du backend), sans redémarrer Claude Code et sans consommer votre usage Claude. Définissez `NEOSWITCH_SAFEGUARD=off` pour désactiver cette vérification, ou lancez Claude Code avec `CLAUDE_CODE_AUTO_MODE_SERVER=0 claude` si vous préférez que Claude vérifie chaque action avec votre connexion Claude (consomme un peu d'usage Claude, avec repli sur Gemini si Claude ne répond pas).
- Quand une version plus récente de NeoSwitch est disponible, la note de session l'indique et donne les commandes de mise à jour. Avec auto-update activé, Claude Code l'installe tout seul, et les fenêtres déjà ouvertes passent à elle dès qu'une nouvelle session Claude Code démarre. Sous Windows, elle s'applique une fois toutes les fenêtres Claude Code fermées puis rouvertes.

## En cas de problème

| Ce que vous voyez | Que faire |
|---|---|
| La note de session dit que NeoSwitch n'est pas encore configuré | Exécutez `/neoswitch:login`. Votre prochain message utilise Gemini 4 Neon. |
| Un message dit que `agy` n'est pas installé | `/neoswitch:login` n'a pas pu l'installer lui-même. Exécutez `curl -fsSL https://antigravity.google/cli/install.sh \| bash` dans un terminal ou installez-le depuis la [page de téléchargement de Google](https://antigravity.google/download), puis exécutez de nouveau `/neoswitch:login`. |
| Chaque message échoue avec une erreur de connexion, ou la note de session indique que cette session ne peut pas répondre | Lancez `/neoswitch:off` : cette session continue avec Claude dès son message suivant, même si la réponse à la commande elle-même échoue. Si les messages échouent encore, tapez `/exit` et relancez `claude` : une nouvelle session utilise Claude, et `/neoswitch:status` vous indique quoi corriger. |
| La note dit que d'autres fenêtres de Claude Code partagent encore la configuration précédente | Lancez `/neoswitch:on` dans cette session : toutes les fenêtres ouvertes passent à sa configuration. Sous Windows, fermez toutes les fenêtres de Claude Code, rouvrez Claude Code, puis lancez `/neoswitch:on`. |
| Un avertissement indique que les connecteurs claude.ai sont désactivés parce qu'une autre source d'authentification est définie | NeoSwitch ne le provoque plus ; une version antérieure le faisait. Mettez le plugin à jour (voir plus bas). S'il persiste, un autre outil définit `ANTHROPIC_AUTH_TOKEN` ou `ANTHROPIC_API_KEY` pour Claude Code. |
| La note dit qu'un autre programme utilise le port dont NeoSwitch a besoin | Fermez ce programme et relancez Claude Code, ou exécutez `/neoswitch:off`. |
| La note dit que NeoSwitch n'a pas pu télécharger son programme | Vérifiez votre connexion internet et relancez Claude Code. Si NeoSwitch a déjà fonctionné sur cet ordinateur, il continue d'utiliser cette version antérieure jusqu'à ce que le téléchargement réussisse. |
| `/neoswitch:off` lui-même indique que NeoSwitch n'a pas pu s'exécuter | Il liste les entrées à supprimer du `.claude/settings.local.json` du projet ; supprimez-les et ouvrez une nouvelle session. L'étape 1 de [supprimer NeoSwitch](#désactiver-mettre-à-jour-ou-supprimer) liste les mêmes entrées. |
| Claude Code ne se connecte plus dans un projet après la suppression de NeoSwitch | Le projet pointe toujours vers NeoSwitch. Supprimez ses entrées de `.claude/settings.local.json`, comme le décrit l'étape 1 de [supprimer NeoSwitch](#désactiver-mettre-à-jour-ou-supprimer), puis ouvrez une nouvelle session. |
| `/neoswitch:login` est une commande inconnue | Mettez le plugin à jour (voir ci-dessous), redémarrez Claude Code, puis exécutez `/reload-plugins`. |
| Un message indique que l'offre Antigravity de votre compte Google ne propose pas gemini-4-neon | Votre offre n'inclut pas Gemini 4 Neon : NeoSwitch ne peut pas répondre avec lui et laisse le projet sur Claude. Relancez `/neoswitch:login` pour vous connecter avec un compte dont l'offre l'inclut, ou lancez `/neoswitch:off` et ouvrez une nouvelle session pour revenir à Claude. |

## Désactiver, mettre à jour ou supprimer

- `/neoswitch:off` ramène cette session à Claude dès son message suivant, avec votre propre connexion Claude. `/neoswitch:on` la rebascule sur Gemini 4 Neon sans rien reconfigurer. Fermer une session met fin à sa bascule : chaque nouvelle session démarre sur Claude.

Pour mettre à jour à la main, exécutez ceci dans un terminal, puis ouvrez une nouvelle session Claude Code : les fenêtres déjà ouvertes passent aussi à la nouvelle version (sous Windows, fermez toutes les fenêtres Claude Code et rouvrez-les) :

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
| Réglages et journal : `proxy.json` (adresse, clé d'accès et voie choisie) et `daemon.log` | `NEOSWITCH_STATE_DIR` s'il est défini ; sinon `~/Library/Application Support/neoswitch/` sous macOS et `$XDG_CONFIG_HOME/neoswitch/` ou `~/.config/neoswitch/` sous Linux |

La connexion Google est conservée par `agy`, pas par NeoSwitch, et reste en place ; `agy` lui-même aussi, même si `/neoswitch:login` l'a installé.

## Ce que NeoSwitch modifie

Tant qu'une session d'un projet utilise Gemini 4 Neon, NeoSwitch ajoute trois entrées à l'objet `env` du `.claude/settings.local.json` de ce projet, et les retire dès qu'aucune session n'y utilise plus NeoSwitch :

- `ANTHROPIC_BASE_URL`, `ANTHROPIC_CUSTOM_HEADERS` et `NODE_EXTRA_CA_CERTS` envoient les requêtes Claude Code du projet à NeoSwitch sur cet ordinateur, par une connexion chiffrée à laquelle seul NeoSwitch peut répondre. NeoSwitch répond avec Gemini 4 Neon aux sessions que vous avez basculées et transmet toute autre requête à Claude sans la modifier, avec votre propre connexion Claude, qu'il ne conserve ni ne journalise. L'en-tête contient une clé qui n'ouvre que NeoSwitch sur cet ordinateur. Si le fichier contenait votre propre `NODE_EXTRA_CA_CERTS` ou `ANTHROPIC_CUSTOM_HEADERS`, NeoSwitch en garde une copie et remet votre valeur quand il retire ses entrées.

Dans un projet Git, NeoSwitch ajoute aussi `.claude/settings.local.json` à la liste d'exclusion propre au clone (`.git/info/exclude`), pour que le jeton reste hors des commits ; votre `.gitignore` n'est pas modifié.

NeoSwitch ne tourne que sur cet ordinateur et n'accepte que les connexions qui en proviennent. Il n'enregistre ni prompts, ni réponses, ni clés, ni informations de connexion, et aucun serveur à nous n'intervient. La connexion Google reste dans `agy` ; NeoSwitch ne la lit jamais.

## Pour aller plus loin

Ce qui fonctionne et ce qui ne fonctionne pas, le fonctionnement de NeoSwitch, l'outil en ligne de commande autonome, les variables d'environnement, la feuille de route et le développement : [docs/cli.md](../docs/cli.md) (en anglais). Voir aussi : [détails de la connexion Antigravity](../docs/antigravity.md) et le [contrat du protocole](../docs/protocol-contract.md).

Consultez [CONTRIBUTING.md](../CONTRIBUTING.md) pour contribuer, [SECURITY.md](../SECURITY.md) pour signaler une vulnérabilité en privé et [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md) pour le code de conduite.

MIT License
