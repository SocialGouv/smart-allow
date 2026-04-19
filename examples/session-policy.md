# Politique de session — exemple

Déposer ce fichier à `<projet>/.claude/session-policy.md` pour overrider la policy
globale sur ce projet précis. Le classifieur le charge en priorité (voir
`home/.claude/hooks/classify-command.py`, fonction `load_policy`).

## Contexte projet
- Nom : my-sensitive-project
- Clusters ciblés : staging-eu-west-1 (sûr), **prod-eu-west-1 (CRITIQUE)**
- Namespaces k8s critiques : prod, prod-payments, prod-auth

## Auto-approuver (decision: approve)
- Lectures locales standard (cat, head, tail, ls, grep, rg, find, stat)
- git status, log, diff, show, branch
- Tests locaux : pytest, npm test (jamais avec --update-snapshot)
- kubectl get / describe / logs **UNIQUEMENT dans namespaces : default, staging***

## Demander confirmation (decision: ask)
- Toute commande kubectl touchant un namespace "prod*"
- Installations : pip install, npm install, apt
- Écritures hors du répertoire projet

## Refuser (decision: deny)
- Toute modification (apply/patch/delete/scale) dans les namespaces prod, prod-payments, prod-auth
- `kubectl config use-context prod-*` (changement de contexte prod)
- Écriture dans ~/.kube/, ~/.aws/, ~/.ssh/
- rm -rf en dehors de /tmp et du projet
