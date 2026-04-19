# Politique : strict

Mode défensif maximal. En cas de doute, toujours ask.

## Auto-approuver (decision: approve)
- Uniquement les lectures PURES et LOCALES : cat, head, tail, ls, pwd, echo, which, file, stat
- git status, git log, git diff (sans --cached sur remote branches)
- kubectl get / describe / logs UNIQUEMENT dans les namespaces listés dans session-policy.md

## Tout le reste → ask ou deny
- Aucune écriture auto-approuvée, même dans le projet
- Aucune installation auto-approuvée
- Aucune commande kubectl qui modifie l'état
- Aucune connexion SSH/SCP sortante auto-approuvée

## Deny systématique
- Toute commande contenant rm -rf
- Toute commande vers un namespace/cluster marqué "prod" dans session-policy.md
- Toute désactivation/modification des hooks eux-mêmes
