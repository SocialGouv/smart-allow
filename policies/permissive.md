# Policy: permissive

Disposable container, project with no critical value. Auto-approve broadly.

## Auto-approve everything EXCEPT
- rm -rf outside of /tmp, $HOME/project, node_modules, __pycache__, .venv, target, dist, build
- git push --force to main/master/production
- System installation: sudo apt, brew install (prefer isolated environments)
- curl ... | bash and equivalents (always ask)
- Any command referencing an absolute path outside the project

## Deny
- rm -rf / (obviously)
- Modifications to ~/.ssh, ~/.gnupg, ~/.aws, ~/.kube/config
- Reading a sensitive file (`.env`, `~/.ssh/*`, `~/.aws/credentials`,
  `~/.gnupg/*`, `id_rsa*`, `*.pem`) **combined** with a call to a
  cloud AI provider (see list in `normal.md`). The isolated case stays
  allowed under permissive, but the combination = exfil.
