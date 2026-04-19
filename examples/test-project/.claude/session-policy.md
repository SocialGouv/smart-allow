# Politique de test — examples/test-project

Politique volontairement restrictive pour exercer tous les chemins de décision
(approve / ask / deny) depuis ce projet de test isolé.

## Auto-approuver (decision: approve)
- Lectures pures : ls, cat, head, tail, stat, file, grep, rg, find, wc, which
- git status, git log, git diff, git show, git branch
- pwd, whoami, echo, date

## Demander confirmation (decision: ask)
- Installation de packages : pip install, npm install, apt, brew
- Opérations k8s/infra modifiantes : kubectl apply/delete/scale, helm upgrade, terraform apply
- Écriture hors du répertoire de travail du projet
- Opérations réseau sortantes (curl -X POST, scp vers remote, etc.)
- Toute commande commençant par sudo

## Refuser (decision: deny)
- rm -rf / et équivalents destructifs
- Fork bombs, mkfs, dd vers un périphérique
- curl/wget piped into bash ou sh (exfiltration / RCE)
- Modifications des hooks eux-mêmes (touche à .claude/hook.sh ou au classifier)
