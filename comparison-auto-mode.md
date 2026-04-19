# smart-allow vs. Claude Code Auto Mode

Comparaison entre **smart-allow** (ce repo : hook `PreToolUse` + LLM local Ollama + policy Markdown) et le **Auto Mode** officiel d'Anthropic annoncé en avril 2026.

Sources :
- [Claude Code Auto Mode — claude.com/blog/auto-mode](https://claude.com/blog/auto-mode)
- [SFEIR Institute — Claude Code Auto Mode, permissions et autonomie](https://institute.sfeir.com/fr/articles/claude-code-auto-mode-permissions-autonomie/)

## TL;DR

| | smart-allow | Auto Mode |
|---|---|---|
| Moteur de décision | LLM local (Ollama) + fast-path déterministe | Classifieur Claude Sonnet 4.6 hébergé par Anthropic |
| Localité | 100 % local (commande + policy ne quittent pas la machine) | Appel API sortant à chaque décision |
| Policy | Markdown versionnable, switchable par session | Règles Anthropic opaques + allow/deny utilisateur |
| Portée | PreToolUse Bash (extensible Edit/Write) | Toutes actions agent (Bash, Write, etc.) |
| Coût marginal | 0 (hors électricité/GPU) | Tokens API par classification |
| Prérequis | Claude Code quelconque + Ollama | Plan Team+ activé par l'admin + Sonnet/Opus 4.6 |
| Fail-safe | LLM indispo → `ask` (jamais d'approve silencieux) | Escalade au prompt humain après 3 blocages consécutifs ou 20 cumulés |
| Auditabilité | Log JSON append-only par décision | Opaque côté utilisateur |

Les deux approches **ne sont pas mutuellement exclusives** — voir §"Combiner les deux".

---

## 1. Moteur de décision

### Auto Mode
Classifieur LLM dédié, hébergé côté Anthropic, basé sur **Claude Sonnet 4.6**. Il reçoit la transcription de conversation + l'appel d'outil, et classe selon un ordre strict :
1. règles allow/deny utilisateur,
2. lectures seules + modifications locales → auto-approve,
3. le reste passe au classifieur.

> « un modèle classifieur séparé (Claude Sonnet 4.6) évalue l'action »

Isolation prompt-injection : « Un fichier malveillant lu par Claude ne peut donc pas manipuler le classifieur directement ».

### smart-allow
Pipeline en 4 étages, dans cet ordre :
1. **Fast-path déterministe** — regex/prefix match sur une allow-list (`ls`, `git status`, `kubectl get`…) et une deny-list absolue (`rm -rf /`, fork bomb, `> /dev/sda`…). Aucun LLM invoqué.
2. **Cache local** (TTL 1h) indexé par `hash(commande + policy + modèle)`.
3. **LLM local via Ollama** (`qwen2.5-coder:7b` par défaut) avec policy Markdown incluse dans le prompt, `format=json`, `temperature=0`.
4. **Fail-safe** : si l'étage 3 échoue (Ollama down, timeout, JSON invalide), on tombe sur `ask` — jamais `approve`.

Isolation prompt-injection : le classifieur ne reçoit **que** la commande et la policy — ni la transcription, ni le contenu des fichiers lus par Claude. Protection structurelle similaire à Auto Mode.

### Point clé
smart-allow compense un modèle local plus petit (~7B vs Sonnet 4.6) par un fast-path qui évacue ~80 % des décisions sans LLM, et par une policy explicite qui donne du contexte métier au modèle.

---

## 2. Policy : transparence et personnalisation

### Auto Mode
- Comportement par défaut : décidé par Anthropic, documenté mais **non modifiable** dans le détail.
- Personnalisation : via `permissions.allow` / `permissions.deny` (listes de patterns). Pas de policy en langage naturel.
- Liste de ce qui est bloqué par défaut dans la doc Anthropic ([permission-modes](https://code.claude.com/docs/en/permission-modes#what-the-classifier-blocks-by-default)) : exfiltration de données, RCE (`curl | bash`), déploiements prod, modifs IAM, `git push --force` sur main, etc.

### smart-allow
- 3 policies fournies (`strict.md`, `normal.md`, `permissive.md`) — fichiers Markdown lisibles, éditables, versionnables.
- Override par projet via `<projet>/.claude/session-policy.md`.
- Switch en une commande : `claude-policy strict`.
- Le texte complet de la policy est injecté dans chaque prompt au LLM. Un·e utilisateur·trice peut écrire :
  > « Interdire toute commande `kubectl` dans les namespaces commençant par `prod-` »
  et le modèle l'appliquera.

### Point clé
smart-allow échange la garantie d'un modèle plus capable contre le contrôle explicite des règles. Auto Mode fait l'inverse.

---

## 3. Localité, confidentialité, coût

### Auto Mode
- Chaque décision implique un round-trip vers l'API Claude ⇒ la commande, potentiellement la transcription, quittent la machine.
- Facturation : non détaillée publiquement, mais c'est du temps Claude Sonnet 4.6 au bas mot.
- Réservé au plan Team+ (Enterprise/API « forthcoming »).

### smart-allow
- Rien ne sort de la machine. Policy + commande → Ollama local → décision.
- Coût marginal : 0. Coût fixe : ~4.5 GB VRAM/RAM pour `qwen2.5-coder:7b`.
- Marche avec n'importe quel plan Claude (y compris individuel), et même en air-gap une fois le modèle pullé.
- Latence : 300–600 ms sur un M2 Pro, plus vite qu'une route-trip réseau pour des commandes simples.

### Point clé
En env régulé (données santé, défense, prestations client avec NDA), la différence localité/cloud n'est pas cosmétique : Auto Mode envoie le tooling à un tiers, smart-allow non.

---

## 4. Portée des actions couvertes

### Auto Mode
Toutes les actions de l'agent : **Write, Bash, fetch HTTP, etc.** Le classifieur voit chaque outil.

### smart-allow
**Uniquement `Bash` via `PreToolUse`** dans son état actuel. C'est volontaire (focus sur le vecteur le plus risqué) et extensible — la section « Évolutions possibles » de [plan-ollama-classifier.md](plan-ollama-classifier.md#annexe-c--évolutions-possibles) prévoit d'ajouter un matcher pour `Edit`/`Write` afin de protéger `~/.ssh`, `/etc`, les fichiers de secrets.

### Point clé
Auto Mode couvre plus de surface out-of-the-box. smart-allow traite en priorité le vecteur d'exécution shell ; étendre à l'écriture de fichiers est ~100 lignes de hook similaire.

---

## 5. Fail-safe et gestion des erreurs

### Auto Mode
- Bascule vers prompts humains **après 3 blocages classifieur consécutifs ou 20 cumulés** par session.
- Si le classifieur est incertain, il peut laisser passer : « le classifieur peut laisser passer des actions risquées ».
- Anthropic recommande explicitement de compléter par du **containment** (devcontainer, VM, sandbox). Le classifieur n'est pas une garantie.

### smart-allow
- Fail-safe strict : **tout ce qui n'est pas une réussite explicite du LLM retombe sur `ask`**. Jamais sur `approve`.
- Pas d'escalade compteur — si les décisions sont mauvaises, l'humain les verra une à une.
- Idem : le classifieur local est vulnérable aux modèles médiocres et aux policies trop laxistes. Le containment reste une bonne pratique.

### Point clé
Les deux convergent sur l'idée que **le classifieur n'est pas un périmètre de sécurité — c'est un filtre UX**. Il fait taire les prompts inutiles ; le containment fait le vrai boulot.

---

## 6. Auditabilité

### Auto Mode
- Décisions affichées dans l'UI Claude Code au moment où elles tombent.
- Pas de log structuré standard documenté pour rejouer ou auditer a posteriori à l'échelle.

### smart-allow
- `~/.claude/classifier.log` (ou chemin custom via `CLAUDE_CLASSIFIER_LOG`) contient une ligne JSON par décision :
  ```json
  {"cmd":"kubectl apply -f x.yaml","decision":"ask","via":"ollama",
   "model":"qwen2.5-coder:7b","policy":"/home/u/.../session-policy.md","ts":1713456000}
  ```
- Requêtes `jq` fournies dans le README pour agréger, lister les « ask » récurrents, dégager des candidats au fast-path.

### Point clé
smart-allow permet une **boucle d'amélioration continue** de la policy via les logs — difficile à faire équivalent avec Auto Mode sans scraper l'UI.

---

## 7. Prérequis & friction d'adoption

| | smart-allow | Auto Mode |
|---|---|---|
| Plan Claude | Tous | Team, Enterprise (API à venir) |
| Modèle Claude | Indifférent | Sonnet 4.6 ou Opus 4.6 |
| Activation admin | Non | Oui |
| Install | `./install-host-ollama.sh && ./install.sh` (~2 min hors téléchargement modèle) | Toggle UI ou `--enable-auto-mode` |
| Téléchargement | ~4.5 GB (modèle) | 0 |
| Compat devcontainer | Natif (DevPod/VSCodium OK) | Natif |

---

## 8. Combiner les deux

Les hooks `PreToolUse` s'exécutent **avant** le mécanisme de permissions intégré de Claude Code. Donc :

```
Bash command proposée par Claude
        │
        ▼
  smart-allow hook (PreToolUse)
        │   ├── fast-path deny ──► bloqué ici, Auto Mode ne voit rien
        │   ├── fast-path approve ─► passe à l'étage suivant
        │   └── ask ───────────────► prompt à l'utilisateur
        ▼
  Auto Mode classifier (Anthropic)
        │   ├── bloque ─► prompt utilisateur
        │   └── laisse passer ─► exécution
```

Cas d'usage de la superposition :
- **Defense in depth** : smart-allow applique la policy métier (namespaces k8s, secrets, volumes), Auto Mode capture les patterns génériques (exfil, RCE).
- **Coût** : le fast-path de smart-allow évite ~80 % des appels Anthropic → réduit la facture Auto Mode si celle-ci compte les tokens classifier.
- **Localité pour les sensibles** : en mode `strict`, smart-allow bloque tout ce qui est sensible avant même qu'Auto Mode (donc Anthropic) ne voie la commande.

Limitation connue : Auto Mode couvre `Write`/`Edit` et d'autres outils, pas smart-allow (pour l'instant). Pour ceux-ci, Auto Mode reste seul au poste.

---

## 9. Matrice décisionnelle

**Préférer Auto Mode si** :
- Vous êtes sur Team+, vous avez Sonnet/Opus 4.6 dispo, et l'envoi des commandes à Anthropic ne pose pas de problème.
- Vous voulez une couverture transverse (Bash + Write + Edit + MCP tools) sans maintenir de config.
- Vous acceptez de faire confiance au jugement d'Anthropic sur ce qui est sûr.

**Préférer smart-allow si** :
- Contrainte réglementaire ou de confidentialité empêche le classifier d'être dans le cloud.
- Vous voulez une policy explicite et versionnée, variable par projet (prod vs dev, namespaces k8s ciblés…).
- Vous êtes sur un plan Claude sans Auto Mode, ou vous voulez gratter la facture de tokens.
- Vous avez besoin d'un log structuré pour audit / calibration.

**Utiliser les deux si** :
- Vous voulez appliquer une policy métier locale **et** garder le filet de sécurité d'Anthropic pour les catégories génériques.
- Vous tournez dans un devcontainer long-lived avec Auto Mode activé et une policy smart-allow `strict` sur les namespaces prod.

---

## 10. Limitations partagées et garde-fous

Les deux classifieurs :
1. **Peuvent se tromper** — faux positifs (bloquent du légitime) et faux négatifs (laissent passer du risqué).
2. **Ne remplacent pas le containment** — devcontainer + user non-root + FS read-only sur les chemins sensibles restent le vrai périmètre.
3. **Dépendent de la qualité du modèle** — petit modèle local lent à raisonner sur des enchaînements complexes ; grand modèle cloud opaque sur ce qu'il considère « sûr ».
4. **Sont contournables par exfiltration lente** — des commandes bénignes individuelles peuvent composer une attaque. Aucun classifieur 1-shot ne détecte ça.

En clair : un classifieur, local ou cloud, est un **outil de confort et de défense en profondeur**, pas une frontière de sécurité.

---

## 11. Conclusion

Auto Mode et smart-allow visent le même but — réduire le nombre de `[y/N]` manuels sans transformer Claude Code en loup libre — mais à partir de compromis opposés :

- **Auto Mode** mise sur un gros modèle hébergé et des règles opaques mais maintenues par Anthropic → clé en main.
- **smart-allow** mise sur un petit modèle local + une policy explicite → contrôle, confidentialité, coût marginal nul.

Le fait que `PreToolUse` laisse les deux coexister rend le choix moins binaire : on peut techniquement avoir un **fast-path local strict** avant un **classifieur cloud souple**, et tirer le meilleur des deux.
