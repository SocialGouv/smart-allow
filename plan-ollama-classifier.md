# Plan : classifieur de commandes via Ollama pour Claude Code

Ce plan met en place un **hook PreToolUse** qui utilise un LLM local servi par **Ollama** pour décider si une commande Bash générée par Claude Code doit être auto-approuvée, soumise à confirmation humaine, ou refusée. Il prévoit aussi l'accès au serveur Ollama de la machine hôte depuis un **devcontainer** utilisé via **DevPod + VSCodium**.

> **Public** : agent exécutant automatiquement les étapes. Chaque étape contient une commande ou un fichier à créer, avec une vérification.

---

## Vue d'ensemble

```
Claude Code (dans devcontainer)
        │
        │ PreToolUse Bash
        ▼
  classify-command.py ──► Ollama hôte (http://host.docker.internal:11434)
        │                          │
        │                          └─► qwen2.5-coder:7b (ou autre)
        ▼
  {"decision": "approve" | "ask" | "deny"}
```

**Principes directeurs** :
1. **Fast-path déterministe** avant tout appel LLM (commandes en lecture seule connues → `approve` direct, patterns toxiques évidents → `deny`).
2. **Policy en Markdown** lue à chaque appel, versionnable et switchable par session.
3. **Fail-safe** : si Ollama ne répond pas ou retourne du bruit, on retombe sur `ask` (on ne désactive jamais la sécurité silencieusement).
4. **Cache local** pour éviter de re-classifier la même commande avec la même policy.
5. **Logs** pour audit a posteriori et calibration de la policy.

---

## Prérequis

- [ ] Ollama installé et fonctionnel sur la machine hôte (`ollama --version` renvoie une version)
- [ ] Au moins un modèle pertinent téléchargé : recommandation `qwen2.5-coder:7b` (bon rapport qualité/vitesse sur du raisonnement sur commandes shell). Alternatives : `llama3.1:8b`, `mistral-small:22b` si GPU costaud.
- [ ] DevPod installé avec provider Docker (ou autre) configuré
- [ ] VSCodium avec extension DevPod
- [ ] Claude Code ≥ version supportant les hooks PreToolUse avec handler `command` (fin 2025 et ultérieur)
- [ ] Python 3.10+ disponible dans le devcontainer

---

## Partie 1 : rendre Ollama hôte accessible depuis le devcontainer

Ollama écoute par défaut uniquement sur `127.0.0.1:11434` côté hôte, donc inaccessible depuis un container. Il faut le reconfigurer pour écouter sur toutes les interfaces, puis résoudre l'adresse de l'hôte depuis l'intérieur du container.

### 1.1 — Faire écouter Ollama sur toutes les interfaces (hôte)

**Sur Linux (systemd)** :

```bash
sudo mkdir -p /etc/systemd/system/ollama.service.d
sudo tee /etc/systemd/system/ollama.service.d/override.conf >/dev/null <<'EOF'
[Service]
Environment="OLLAMA_HOST=0.0.0.0:11434"
Environment="OLLAMA_ORIGINS=*"
EOF
sudo systemctl daemon-reload
sudo systemctl restart ollama
```

**Sur macOS** (app Ollama) :

```bash
launchctl setenv OLLAMA_HOST "0.0.0.0:11434"
launchctl setenv OLLAMA_ORIGINS "*"
# Puis quitter et relancer l'app Ollama depuis la barre de menu
```

**Sur macOS ou Linux en lancement manuel** :

```bash
OLLAMA_HOST=0.0.0.0:11434 ollama serve
```

**Vérification depuis l'hôte** :

```bash
curl -s http://127.0.0.1:11434/api/tags | head -c 200
# doit renvoyer un JSON listant les modèles
```

> ⚠️ **Sécurité** : exposer Ollama sur `0.0.0.0` le rend accessible à tout ce qui peut joindre l'hôte sur ce port. Si tu es sur un LAN de confiance c'est ok. Sinon, filtre par firewall (`ufw`, `pf`, firewall macOS) pour n'autoriser que l'interface Docker (`docker0` sur Linux, généralement 172.17.0.0/16). Exemple Linux :
> ```bash
> sudo ufw allow from 172.16.0.0/12 to any port 11434
> sudo ufw deny 11434
> ```

### 1.2 — Résoudre l'hôte depuis le devcontainer

**Stratégie selon l'OS hôte** :

| OS hôte | Hostname utilisable depuis le container |
|---------|-----------------------------------------|
| macOS (Docker Desktop) | `host.docker.internal` (natif) |
| Windows (Docker Desktop) | `host.docker.internal` (natif) |
| Linux (Docker moderne ≥ 20.10) | `host.docker.internal` **si** `--add-host=host.docker.internal:host-gateway` passé au run |
| Linux (Podman/DevPod avec Podman) | Idem avec `--add-host=host.docker.internal:host-gateway` |

### 1.3 — Configuration DevPod/devcontainer

Ajouter dans `.devcontainer/devcontainer.json` du projet :

```jsonc
{
  "name": "dev-with-ollama-host",
  "image": "mcr.microsoft.com/devcontainers/base:ubuntu",

  // Injecte host.docker.internal pour Linux. No-op sur macOS/Windows (déjà présent).
  "runArgs": [
    "--add-host=host.docker.internal:host-gateway"
  ],

  // Variables exposées à Claude Code à l'intérieur
  "containerEnv": {
    "OLLAMA_HOST": "http://host.docker.internal:11434"
  },

  "features": {
    "ghcr.io/devcontainers/features/python:1": {
      "version": "3.12"
    }
  },

  "postCreateCommand": "pip install --user requests"
}
```

**Pour DevPod spécifiquement**, si tu utilises un `devpod.yaml` ou flags CLI, l'équivalent est :

```bash
devpod up . --provider docker \
  --ide vscodium \
  --devcontainer-image mcr.microsoft.com/devcontainers/base:ubuntu
```

DevPod lit automatiquement `.devcontainer/devcontainer.json`, donc les `runArgs` ci-dessus s'appliquent. Vérifie avec :

```bash
devpod status
docker inspect $(docker ps -qf "label=devpod.workspace") | grep -A2 ExtraHosts
```

### 1.4 — Vérification depuis l'intérieur du container

Une fois le container up (`devpod up` puis `devpod ssh` ou via VSCodium) :

```bash
# Test basique de connectivité
curl -s http://host.docker.internal:11434/api/tags | head -c 200

# Test d'inférence
curl -s http://host.docker.internal:11434/api/generate -d '{
  "model": "qwen2.5-coder:7b",
  "prompt": "Réponds uniquement par OK",
  "stream": false
}' | grep -o '"response":"[^"]*"'
```

Si ces deux commandes renvoient du contenu, la plomberie est bonne. Sinon, voir **Annexe A** en fin de document pour le dépannage.

---

## Partie 2 : installer le classifieur dans le devcontainer

### 2.1 — Structure de fichiers cible

À la fin de cette partie, on aura dans le container (ou monté depuis le projet) :

```
~/.claude/
├── hooks/
│   └── classify-command.py        # le classifieur
├── policies/
│   ├── strict.md                  # politique restrictive
│   ├── normal.md                  # politique équilibrée (défaut)
│   └── permissive.md              # politique large
├── classifier.log                 # journal (append-only)
└── classifier-cache/              # cache TTL 1h
```

Côté projet (versionné git) :

```
<projet>/
└── .claude/
    ├── settings.json              # enregistre le hook
    └── session-policy.md          # override de politique pour CE projet
```

### 2.2 — Créer les politiques de base

**Fichier `~/.claude/policies/normal.md`** :

```markdown
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
```

**Fichier `~/.claude/policies/strict.md`** (sessions prod/cluster critique) :

```markdown
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
```

**Fichier `~/.claude/policies/permissive.md`** (bricolage perso, container jetable) :

```markdown
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
```

### 2.3 — Créer le script classifieur

**Fichier `~/.claude/hooks/classify-command.py`** :

```python
#!/usr/bin/env python3
"""
Classifieur de commandes Bash pour Claude Code, basé sur Ollama.
Lu sur stdin un event JSON PreToolUse, écrit une décision JSON sur stdout.

Ordre de décision :
  1. Fast-path déterministe (commandes clairement safe ou clairement toxiques)
  2. Cache (commande+policy_hash déjà vu < TTL)
  3. LLM local via Ollama
  4. Fail-safe → "ask" si tout échoue
"""
import json
import sys
import os
import hashlib
import time
import re
from pathlib import Path

try:
    import requests
except ImportError:
    # Fail-safe : si requests absent, demander à l'humain
    print(json.dumps({"decision": "ask", "reason": "requests module missing in container"}))
    sys.exit(0)


# ---------- Configuration ----------
OLLAMA_HOST = os.environ.get("OLLAMA_HOST", "http://host.docker.internal:11434")
OLLAMA_MODEL = os.environ.get("CLAUDE_CLASSIFIER_MODEL", "qwen2.5-coder:7b")
OLLAMA_TIMEOUT = int(os.environ.get("CLAUDE_CLASSIFIER_TIMEOUT", "12"))
CACHE_TTL = int(os.environ.get("CLAUDE_CLASSIFIER_CACHE_TTL", "3600"))
HOOK_DEBUG = os.environ.get("CLAUDE_HOOK_DEBUG") == "1"

HOME = Path.home()
CACHE_DIR = HOME / ".claude" / "classifier-cache"
LOG_FILE = HOME / ".claude" / "classifier.log"


# ---------- Fast-path déterministe ----------
SAFE_EXACT = {"pwd", "whoami", "hostname", "date", "uptime", "id"}
SAFE_PREFIXES = (
    "ls ", "ls\n", "ls\t", "cat ", "less ", "head ", "tail ", "stat ", "file ",
    "grep ", "rg ", "egrep ", "fgrep ", "find ", "wc ", "which ", "whereis ",
    "echo ", "printf ",
    "git status", "git log", "git diff", "git show", "git branch", "git remote",
    "git blame", "git reflog", "git stash list", "git config --get",
    "docker ps", "docker logs ", "docker inspect ", "docker images",
    "kubectl get ", "kubectl describe ", "kubectl logs ", "kubectl top ",
    "kubectl events", "kubectl version", "kubectl config view",
    "helm list", "helm history", "helm get ", "helm status ",
    "terraform plan", "terraform show", "terraform state list",
    "npm list", "npm ls", "pip list", "pip show", "cargo tree",
    "python --version", "node --version", "go version",
)

HARD_DENY_SUBSTRINGS = (
    "rm -rf /", "rm -rf /*", "rm -rf /.", "rm -rf ~", "rm -rf $HOME",
    ":(){ :|:& };:",  # fork bomb
    "mkfs.", "mkfs ",
    "dd if=/dev/zero of=/dev/", "dd if=/dev/random of=/dev/",
    "chmod -R 777 /", "chown -R ",
    "> /dev/sda", "> /dev/nvme",
)

DANGEROUS_PATTERNS = (
    r"\|\s*(bash|sh|zsh)\b",          # pipe vers shell
    r"curl\s+[^|]+\|\s*(bash|sh)",    # curl | bash
    r"wget\s+[^|]+\|\s*(bash|sh)",    # wget | bash
    r"eval\s+\$\(",                    # eval avec substitution
)


def fast_path(command: str) -> str | None:
    """Retourne 'approve', 'deny', ou None (incertain → LLM)."""
    cmd = command.strip()
    if not cmd:
        return "approve"

    # Deny absolu
    for pat in HARD_DENY_SUBSTRINGS:
        if pat in cmd:
            return "deny"

    # Patterns dangereux → ne pas fast-path approve, laisser au LLM
    for regex in DANGEROUS_PATTERNS:
        if re.search(regex, cmd):
            return None

    # Approve rapide : commande simple sans redirection/substitution/chaînage
    has_complexity = any(c in cmd for c in [
        "|", "&&", "||", ";", ">", "<", "`", "$(", "&", "bash -c", "sh -c",
        "xargs", "exec", "npx"
    ])

    if cmd in SAFE_EXACT:
        return "approve"

    if not has_complexity:
        for p in SAFE_PREFIXES:
            if cmd == p.strip() or cmd.startswith(p):
                return "approve"

    return None


# ---------- Policy ----------
def load_policy(project_dir: str) -> tuple[str, str]:
    """Cherche la policy active, par ordre de priorité."""
    candidates = [
        Path(project_dir) / ".claude" / "session-policy.md",
        Path(project_dir) / ".claude" / "policy.md",
        HOME / ".claude" / "active-policy.md",  # symlink vers strict/normal/permissive
        HOME / ".claude" / "policies" / "normal.md",
    ]
    for p in candidates:
        if p.exists():
            return p.read_text(), str(p)
    return ("Politique par défaut : demander confirmation pour toute action "
            "destructive ou qui touche des ressources hors du répertoire courant."), "<default>"


# ---------- Cache ----------
def cache_key(command: str, policy_hash: str, model: str) -> str:
    h = hashlib.sha256(f"{command}::{policy_hash}::{model}".encode()).hexdigest()
    return h[:16]


def cache_get(key: str) -> dict | None:
    CACHE_DIR.mkdir(parents=True, exist_ok=True)
    f = CACHE_DIR / f"{key}.json"
    if not f.exists():
        return None
    if time.time() - f.stat().st_mtime > CACHE_TTL:
        return None
    try:
        return json.loads(f.read_text())
    except Exception:
        return None


def cache_set(key: str, decision: dict) -> None:
    CACHE_DIR.mkdir(parents=True, exist_ok=True)
    (CACHE_DIR / f"{key}.json").write_text(json.dumps(decision))


# ---------- LLM call (Ollama) ----------
SYSTEM_PROMPT = """Tu es un classifieur de sécurité pour un agent de coding autonome.

Tu reçois une commande shell et une politique de sécurité. Tu dois décider entre :
- "approve" : la commande est sûre ET conforme à la politique. Aucun humain n'a besoin de la valider.
- "ask" : la commande n'est pas clairement interdite mais a un effet notable, irréversible, ou sort du périmètre évident. Demander confirmation à l'humain.
- "deny" : la commande est clairement interdite par la politique ou manifestement dangereuse.

RÈGLES IMPORTANTES :
1. En cas de doute, réponds "ask". Ne jamais "approve" si tu n'es pas sûr.
2. Pour les commandes chaînées (pipes, &&, ;), la décision est la plus restrictive de toutes les sous-commandes.
3. Pour les wrappers (bash -c, sh -c, xargs, npx, docker exec) : analyse la commande INTERNE, pas le wrapper.
4. Méfie-toi des obfuscations : variables de substitution, base64, eval, commandes en plusieurs mots collés.

Réponds UNIQUEMENT avec un objet JSON sur une seule ligne, sans texte autour, sans backticks :
{"decision":"approve","reason":"..."} OU {"decision":"ask","reason":"..."} OU {"decision":"deny","reason":"..."}

"reason" doit être court (< 120 caractères), en français, expliquant le critère déclencheur."""


def call_ollama(command: str, policy: str, cwd: str) -> dict:
    user_prompt = f"""POLITIQUE ACTIVE :
{policy}

---

WORKING DIRECTORY : {cwd}

COMMANDE À CLASSIFIER :
{command}

Réponds par UN SEUL objet JSON, rien d'autre."""

    payload = {
        "model": OLLAMA_MODEL,
        "system": SYSTEM_PROMPT,
        "prompt": user_prompt,
        "stream": False,
        "format": "json",   # force Ollama à sortir du JSON parseable
        "options": {
            "temperature": 0.0,
            "num_predict": 150,
        },
    }

    r = requests.post(
        f"{OLLAMA_HOST}/api/generate",
        json=payload,
        timeout=OLLAMA_TIMEOUT,
    )
    r.raise_for_status()
    response_text = r.json().get("response", "").strip()

    # Avec format=json, la réponse est censée être du JSON direct
    try:
        parsed = json.loads(response_text)
    except json.JSONDecodeError:
        # Fallback : extraire un JSON du texte
        m = re.search(r"\{[^{}]*\"decision\"[^{}]*\}", response_text)
        if not m:
            raise ValueError(f"No JSON in response: {response_text[:200]}")
        parsed = json.loads(m.group())

    decision = parsed.get("decision")
    if decision not in ("approve", "ask", "deny"):
        raise ValueError(f"Invalid decision: {decision}")

    return {
        "decision": decision,
        "reason": parsed.get("reason", "")[:200],
    }


# ---------- Logging ----------
def log_event(record: dict) -> None:
    LOG_FILE.parent.mkdir(parents=True, exist_ok=True)
    record["ts"] = time.time()
    with LOG_FILE.open("a") as f:
        f.write(json.dumps(record, ensure_ascii=False) + "\n")


def debug(msg: str) -> None:
    if HOOK_DEBUG:
        print(f"[classifier] {msg}", file=sys.stderr)


# ---------- Main ----------
def main() -> None:
    try:
        event = json.load(sys.stdin)
    except json.JSONDecodeError as e:
        # Sans event on ne peut rien dire, par sécurité on demande
        print(json.dumps({"decision": "ask", "reason": f"invalid hook input: {e}"}))
        return

    tool_input = event.get("tool_input", {})
    command = tool_input.get("command", "")
    cwd = event.get("cwd", os.getcwd())
    project_dir = os.environ.get("CLAUDE_PROJECT_DIR", cwd)

    if not command:
        print(json.dumps({"decision": "approve", "reason": "empty command"}))
        return

    # 1. Fast-path
    fast = fast_path(command)
    if fast == "approve":
        debug(f"fast-path APPROVE: {command[:80]}")
        print(json.dumps({"decision": "approve", "reason": "fast-path: safe prefix"}))
        log_event({"cmd": command, "decision": "approve", "via": "fast-path"})
        return

    if fast == "deny":
        debug(f"fast-path DENY: {command[:80]}")
        decision = {"decision": "deny", "reason": "fast-path: hard-deny pattern"}
        print(json.dumps(decision))
        log_event({"cmd": command, "decision": "deny", "via": "fast-path"})
        return

    # 2. Charger policy
    policy, policy_source = load_policy(project_dir)
    policy_hash = hashlib.sha256(policy.encode()).hexdigest()[:12]

    # 3. Cache
    key = cache_key(command, policy_hash, OLLAMA_MODEL)
    cached = cache_get(key)
    if cached:
        debug(f"cache HIT: {cached}")
        print(json.dumps(cached))
        log_event({"cmd": command, "decision": cached["decision"], "via": "cache",
                   "policy": policy_source})
        return

    # 4. LLM Ollama
    try:
        decision = call_ollama(command, policy, cwd)
        cache_set(key, decision)
        debug(f"llm: {decision}")
        print(json.dumps(decision))
        log_event({"cmd": command, "decision": decision["decision"],
                   "reason": decision.get("reason"), "via": "ollama",
                   "model": OLLAMA_MODEL, "policy": policy_source})
    except Exception as e:
        # Fail-safe : pas d'auto-approve si le classifieur est cassé
        debug(f"ollama FAILED: {e}")
        fallback = {"decision": "ask", "reason": f"classifier unavailable: {str(e)[:80]}"}
        print(json.dumps(fallback))
        log_event({"cmd": command, "decision": "ask", "via": "fail-safe",
                   "error": str(e)[:200]})


if __name__ == "__main__":
    main()
```

### 2.4 — Enregistrer le hook dans Claude Code

**Fichier `~/.claude/settings.json`** (ou `<projet>/.claude/settings.json` pour scope projet) :

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "python3 ~/.claude/hooks/classify-command.py",
            "timeout": 15000
          }
        ]
      }
    ]
  }
}
```

### 2.5 — Rendre exécutable

```bash
chmod +x ~/.claude/hooks/classify-command.py
# Test rapide hors Claude Code :
echo '{"tool_input":{"command":"ls -la"},"cwd":"/tmp"}' | python3 ~/.claude/hooks/classify-command.py
# Résultat attendu : {"decision": "approve", "reason": "fast-path: safe prefix"}

echo '{"tool_input":{"command":"kubectl apply -f prod.yaml"},"cwd":"/tmp"}' | python3 ~/.claude/hooks/classify-command.py
# Résultat attendu : {"decision": "ask", "reason": "..."} (via LLM)
```

---

## Partie 3 : workflow quotidien

### 3.1 — Switcher de politique en une commande

Créer un petit utilitaire :

**Fichier `~/.claude/bin/claude-policy`** :

```bash
#!/usr/bin/env bash
set -euo pipefail
POLICY="${1:-}"
DIR="$HOME/.claude/policies"
ACTIVE="$HOME/.claude/active-policy.md"

case "$POLICY" in
    strict|normal|permissive)
        ln -sf "$DIR/$POLICY.md" "$ACTIVE"
        echo "Policy active : $POLICY"
        echo "Symlink : $ACTIVE -> $DIR/$POLICY.md"
        ;;
    show|"")
        if [ -L "$ACTIVE" ]; then
            readlink "$ACTIVE"
        else
            echo "Aucune policy active (fallback = normal)"
        fi
        ;;
    list)
        ls "$DIR"
        ;;
    *)
        echo "Usage: claude-policy [strict|normal|permissive|show|list]"
        exit 1
        ;;
esac
```

```bash
chmod +x ~/.claude/bin/claude-policy
echo 'export PATH="$HOME/.claude/bin:$PATH"' >> ~/.bashrc  # ou ~/.zshrc
```

Utilisation : `claude-policy strict` avant d'attaquer un cluster prod, `claude-policy permissive` pour bricoler.

### 3.2 — Override par projet

Dans un projet sensible, poser `.claude/session-policy.md` qui détaille précisément le contexte (liste des namespaces k8s sensibles, paths à protéger…). Ce fichier prime sur le symlink global.

### 3.3 — Audit

```bash
# Voir les 20 dernières décisions
tail -20 ~/.claude/classifier.log | jq -c '{cmd, decision, via}'

# Stats d'utilisation sur la journée
jq -s 'group_by(.decision) | map({decision: .[0].decision, count: length})' \
   ~/.claude/classifier.log

# Commandes qui ont été en "ask" (candidates à ajouter au fast-path ou à la policy)
jq -c 'select(.decision == "ask") | .cmd' ~/.claude/classifier.log | sort -u
```

---

## Partie 4 : validation end-to-end

Test complet à exécuter dans le devcontainer après installation :

- [ ] `curl -s http://host.docker.internal:11434/api/tags` renvoie la liste des modèles
- [ ] Fast-path approve : `echo '{"tool_input":{"command":"ls"},"cwd":"/tmp"}' | python3 ~/.claude/hooks/classify-command.py` → `approve`
- [ ] Fast-path deny : `echo '{"tool_input":{"command":"rm -rf /"},"cwd":"/tmp"}' | python3 ~/.claude/hooks/classify-command.py` → `deny`
- [ ] LLM call : `echo '{"tool_input":{"command":"kubectl apply -f deploy.yaml"},"cwd":"/tmp"}' | python3 ~/.claude/hooks/classify-command.py` → `ask` (via ollama, visible dans le log)
- [ ] Cache : relancer la même commande → résultat instantané, `via: cache` dans le log
- [ ] Policy switch : `claude-policy strict` puis refaire un `ls` → en mode strict, un `ls` complexe peut devenir `ask`
- [ ] Fail-safe : `OLLAMA_HOST=http://nonexistent:11434 echo '{"tool_input":{"command":"npm install foo"},"cwd":"/tmp"}' | python3 ~/.claude/hooks/classify-command.py` → `ask` avec `via: fail-safe`
- [ ] Intégration Claude Code : lancer `claude` dans le devcontainer, lui demander de lancer une commande, vérifier que le prompt d'autorisation disparaît pour `ls` mais apparaît pour `kubectl apply`

---

## Annexe A — Dépannage accès Ollama hôte

| Symptôme | Cause probable | Solution |
|----------|----------------|----------|
| `curl: (7) Failed to connect to host.docker.internal port 11434` depuis le container | `--add-host=host.docker.internal:host-gateway` absent sur Linux | Ajouter dans `runArgs` du devcontainer |
| `Connection refused` même depuis l'hôte sur 127.0.0.1:11434 | Ollama pas lancé | `systemctl status ollama` / relancer l'app |
| `Connection refused` depuis le container mais ok depuis l'hôte | Ollama écoute uniquement sur 127.0.0.1 | Reconfigurer avec `OLLAMA_HOST=0.0.0.0:11434` (voir 1.1) |
| `403 Forbidden` | `OLLAMA_ORIGINS` trop restrictif | Mettre `OLLAMA_ORIGINS=*` (ou l'origine du container) |
| Réponse très lente (>30s) | Modèle pas encore chargé en VRAM | Pré-charger : `ollama run qwen2.5-coder:7b "hi"` une fois |
| Sur macOS, l'IP du container n'est pas `host.docker.internal` | Rare — autre VM manager | Tester `gateway.docker.internal`, ou utiliser l'IP du bridge : `ip route | awk '/default/ {print $3}'` dans le container |

## Annexe B — Choix du modèle

| Modèle | Taille | Latence indicative (M2 Pro) | Qualité classification |
|--------|--------|------------------------------|------------------------|
| `qwen2.5-coder:7b` | ~4.5 GB | ~300-600 ms | Très bonne, recommandé |
| `qwen2.5:3b` | ~2 GB | ~150-300 ms | Correcte, pour laptop léger |
| `llama3.1:8b` | ~4.7 GB | ~400-700 ms | Bonne, plus bavard |
| `mistral-small:22b` | ~13 GB | ~1-2 s | Excellente, pour workstation GPU |

Télécharger le modèle choisi **sur l'hôte** : `ollama pull qwen2.5-coder:7b`.

## Annexe C — Évolutions possibles

- **Double-modèle** : Haiku-3B d'abord, escalade vers 22B si `confidence < 0.8`. Nécessite d'ajouter un champ `confidence` à la sortie JSON.
- **Mode dry-run** : variable `CLAUDE_CLASSIFIER_DRY_RUN=1` qui log les décisions sans les appliquer (toujours `ask` au final). Utile pour calibrer la policy.
- **Apprentissage** : parser `classifier.log` et les décisions humaines côté Claude Code pour repérer les désaccords classifieur/humain, et affiner la policy.
- **Multi-matcher** : ajouter un hook similaire pour `Edit`/`Write` pour protéger des fichiers sensibles (~/.ssh, /etc, secrets).
