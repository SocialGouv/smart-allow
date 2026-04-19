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
    print(json.dumps({"decision": "ask", "reason": "requests module missing in container"}))
    sys.exit(0)


OLLAMA_HOST = os.environ.get("OLLAMA_HOST", "http://host.docker.internal:11434")
OLLAMA_MODEL = os.environ.get("CLAUDE_CLASSIFIER_MODEL", "qwen2.5-coder:7b")
OLLAMA_TIMEOUT = int(os.environ.get("CLAUDE_CLASSIFIER_TIMEOUT", "12"))
CACHE_TTL = int(os.environ.get("CLAUDE_CLASSIFIER_CACHE_TTL", "3600"))
HOOK_DEBUG = os.environ.get("CLAUDE_HOOK_DEBUG") == "1"

HOME = Path.home()
CACHE_DIR = Path(os.environ.get("CLAUDE_CLASSIFIER_CACHE_DIR", str(HOME / ".claude" / "classifier-cache")))
LOG_FILE = Path(os.environ.get("CLAUDE_CLASSIFIER_LOG", str(HOME / ".claude" / "classifier.log")))


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
    ":(){ :|:& };:",
    "mkfs.", "mkfs ",
    "dd if=/dev/zero of=/dev/", "dd if=/dev/random of=/dev/",
    "chmod -R 777 /", "chown -R ",
    "> /dev/sda", "> /dev/nvme",
)

DANGEROUS_PATTERNS = (
    r"\|\s*(bash|sh|zsh)\b",
    r"curl\s+[^|]+\|\s*(bash|sh)",
    r"wget\s+[^|]+\|\s*(bash|sh)",
    r"eval\s+\$\(",
)


def fast_path(command: str) -> str | None:
    """Retourne 'approve', 'deny', ou None (incertain → LLM)."""
    cmd = command.strip()
    if not cmd:
        return "approve"

    for pat in HARD_DENY_SUBSTRINGS:
        if pat in cmd:
            return "deny"

    for regex in DANGEROUS_PATTERNS:
        if re.search(regex, cmd):
            return None

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


def load_policy(project_dir: str) -> tuple[str, str]:
    """Cherche la policy active, par ordre de priorité."""
    candidates = [
        Path(project_dir) / ".claude" / "session-policy.md",
        Path(project_dir) / ".claude" / "policy.md",
        HOME / ".claude" / "active-policy.md",
        HOME / ".claude" / "policies" / "normal.md",
    ]
    for p in candidates:
        if p.exists():
            return p.read_text(), str(p)
    return ("Politique par défaut : demander confirmation pour toute action "
            "destructive ou qui touche des ressources hors du répertoire courant."), "<default>"


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
        "format": "json",
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

    try:
        parsed = json.loads(response_text)
    except json.JSONDecodeError:
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


def log_event(record: dict) -> None:
    LOG_FILE.parent.mkdir(parents=True, exist_ok=True)
    record["ts"] = time.time()
    with LOG_FILE.open("a") as f:
        f.write(json.dumps(record, ensure_ascii=False) + "\n")


def debug(msg: str) -> None:
    if HOOK_DEBUG:
        print(f"[classifier] {msg}", file=sys.stderr)


# Internally we use approve/ask/deny; Claude Code's PreToolUse expects
# allow/ask/deny/defer nested under hookSpecificOutput.
_DECISION_TO_PERMISSION = {"approve": "allow", "ask": "ask", "deny": "deny"}


def emit(decision: str, reason: str) -> None:
    """Write the JSON payload Claude Code expects on stdout."""
    perm = _DECISION_TO_PERMISSION.get(decision, "ask")
    payload = {
        "hookSpecificOutput": {
            "hookEventName": "PreToolUse",
            "permissionDecision": perm,
            "permissionDecisionReason": reason,
        }
    }
    print(json.dumps(payload, ensure_ascii=False))


def main() -> None:
    try:
        event = json.load(sys.stdin)
    except json.JSONDecodeError as e:
        emit("ask", f"invalid hook input: {e}")
        return

    tool_input = event.get("tool_input", {})
    command = tool_input.get("command", "")
    cwd = event.get("cwd", os.getcwd())
    project_dir = os.environ.get("CLAUDE_PROJECT_DIR", cwd)

    if not command:
        emit("approve", "empty command")
        return

    fast = fast_path(command)
    if fast == "approve":
        debug(f"fast-path APPROVE: {command[:80]}")
        emit("approve", "fast-path: safe prefix")
        log_event({"cmd": command, "decision": "approve", "via": "fast-path"})
        return

    if fast == "deny":
        debug(f"fast-path DENY: {command[:80]}")
        emit("deny", "fast-path: hard-deny pattern")
        log_event({"cmd": command, "decision": "deny", "via": "fast-path"})
        return

    policy, policy_source = load_policy(project_dir)
    policy_hash = hashlib.sha256(policy.encode()).hexdigest()[:12]

    key = cache_key(command, policy_hash, OLLAMA_MODEL)
    cached = cache_get(key)
    if cached:
        debug(f"cache HIT: {cached}")
        emit(cached["decision"], cached.get("reason", ""))
        log_event({"cmd": command, "decision": cached["decision"], "via": "cache",
                   "policy": policy_source})
        return

    try:
        decision = call_ollama(command, policy, cwd)
        cache_set(key, decision)
        debug(f"llm: {decision}")
        emit(decision["decision"], decision.get("reason", ""))
        log_event({"cmd": command, "decision": decision["decision"],
                   "reason": decision.get("reason"), "via": "ollama",
                   "model": OLLAMA_MODEL, "policy": policy_source})
    except Exception as e:
        debug(f"ollama FAILED: {e}")
        emit("ask", f"classifier unavailable: {str(e)[:80]}")
        log_event({"cmd": command, "decision": "ask", "via": "fail-safe",
                   "error": str(e)[:200]})


if __name__ == "__main__":
    main()
