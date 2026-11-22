#!/usr/bin/env python3
"""PreToolUse guard for Bash. Denies what the owner's rules forbid outright and
asks before outward-facing commands. Reads the hook payload on stdin."""
import json
import re
import sys

DENY = [
    (r"(^|[;&|(]\s*|\bsudo\s+|\bxargs\s+)aws\s", "aws CLI is off-limits; infra goes through CDK"),
    (r"~/\.aws|\$HOME/\.aws|/\.aws/(credentials|config|sso)", "reading ~/.aws is off-limits"),
    (r"\bgit\s+add\s+(-A|--all|\.)(\s|$)", "blanket git add; stage specific paths"),
    (r"\bgit\s+rm\s+-r\s+(\.|\*)(\s|$)", "bulk git rm"),
    (r"\b(curl|wget)\b[^|;]*\|\s*(sudo\s+)?(ba|z)?sh\b", "download piped into a shell"),
]
ASK = [
    (r"\bcdk\s+deploy\b|\bnpm\s+run\s+deploy\b|\bcdk\s+destroy\b|\bnpm\s+run\s+destroy\b",
     "this changes production infrastructure"),
    (r"\bgit\s+push\b", "pushing is outward-facing"),
    (r"cmd/seed\b.*(--all|state\.json)", "seeding state rewinds the production fetch cursor"),
]


def decide(command):
    for pattern, reason in DENY:
        if re.search(pattern, command):
            return "deny", reason
    for pattern, reason in ASK:
        if re.search(pattern, command):
            return "ask", reason
    return None, None


def main():
    try:
        payload = json.load(sys.stdin)
    except ValueError:
        return 0
    command = (payload.get("tool_input") or {}).get("command") or ""
    decision, reason = decide(command)
    if decision:
        json.dump({"hookSpecificOutput": {
            "hookEventName": "PreToolUse",
            "permissionDecision": decision,
            "permissionDecisionReason": "guard-bash: " + reason,
        }}, sys.stdout)
    return 0


if __name__ == "__main__":
    sys.exit(main())
