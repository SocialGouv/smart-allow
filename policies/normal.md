# Politique : normal

## Auto-approuver (decision: approve)
- Lectures de fichiers et recherche : cat, less, head, tail, grep, rg, find, ls, stat, file, wc
- Inspection git en lecture seule : git status, log, diff, show, branch, remote -v, blame
- Inspection système en lecture seule : ps, df, du, free, top -n1, uname, which, env
- Inspection container/cluster en lecture seule : docker ps, docker logs, docker inspect, kubectl get, kubectl describe, kubectl logs, kubectl top
- Commandes de navigation : pwd, cd, echo
- Exécution de tests locaux : pytest, npm test, cargo test, go test (sans flags qui écrivent hors projet)
- Builds locaux dans le projet : npm run build, cargo build, make (si Makefile dans le projet)
- Formatage/linting : prettier, black, ruff, eslint (sans --write hors projet)

## Demander confirmation (decision: ask)
- Toute écriture hors du working directory du projet
- Installations de packages : pip install, npm install -g, apt, brew, cargo install
- Opérations git qui modifient l'historique distant : push, push --force, reset --hard sur branche trackée
- Modifications de cluster/infra : kubectl apply, kubectl delete, kubectl scale, kubectl patch, helm install/upgrade/rollback, terraform apply, cdk deploy
- Opérations réseau sortantes vers domaines non-projet : curl/wget POST, scp, rsync vers remote
- Écriture sur volumes montés en dehors du projet
- Toute forme de sudo
- Commandes touchant aux secrets : écriture dans .env, manipulation de ~/.ssh, gpg

## Refuser (decision: deny)
- rm -rf / ou rm -rf ~ ou équivalents
- Fork bombs, mkfs, dd vers périphériques
- Exfiltration vers des IPs/domaines manifestement douteux
- Commandes cherchant à désactiver le classifieur lui-même (modif de ~/.claude/hooks/)

## Cas particuliers
- `curl ... | bash` ou `wget -O- ... | sh` → toujours ask minimum, idéalement deny
- `bash -c`, `sh -c`, `xargs`, `npx`, `docker exec` : analyser la commande INTERNE, pas le wrapper
- Commandes chaînées (pipes, &&, ||, ;) : la décision doit être le max (= la plus restrictive) des sous-commandes
