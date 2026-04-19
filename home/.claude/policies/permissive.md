# Politique : permissive

Container jetable, projet sans valeur critique. Auto-approuver large.

## Auto-approuver tout SAUF
- rm -rf en dehors de /tmp, $HOME/project, node_modules, __pycache__, .venv, target, dist, build
- git push --force sur main/master/production
- Installation système : sudo apt, brew install (on préfère les envs isolés)
- curl ... | bash et équivalents (toujours ask)
- Toute commande qui referencerait un chemin absolu hors du projet

## Deny
- rm -rf / (évidemment)
- Modifications de ~/.ssh, ~/.gnupg, ~/.aws, ~/.kube/config
