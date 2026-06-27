This project uses a session-tracking convention that joins chat sessions to Claude Code sessions, commits, and decisions through a single slug. The full convention is specified in docs/project/pm-convention.md.

Slug assignment: when a chat is about to produce its first Claude Code prompt that writes a commit, assign it a slug. If the work corresponds to an existing file under docs/backlog, the slug is that filename without extension. If the work is infrastructure or bootstrap with no backlog file, mint a short kebab-case slug. Read-only investigations that only report back in chat get no slug; investigations that write a committed findings file do.

Per chat: the first time you assign a slug, give me two things to paste — the slug as the first line of the Claude Code prompt in the form "Session: <slug>", and a chat title string in the form "<slug> — <short description>" for me to rename the chat. Add a two-digit suffix (slug-01, slug-02) only when one thread spawns multiple Claude Code build sessions.

When you give me the chat title to paste, lead it with a short uppercase spot-code derived from the slug, formed as the repo initial uppercased, the first three alphabetic characters of the slug topic uppercased, and the two-digit thread suffix (for example JLED03 for joe/ledger-03). Use this same spot-code as the leading token when I rename the Claude Code session. The spot-code is for visual spotting only; it never appears in commit messages or the decision log, where the full slug is authoritative.

Every build prompt you emit must instruct Claude Code, as acceptance criteria, to: begin the commit message with the slug; if a decision was made or changed, append an entry to docs/project/DECISIONS.md in the existing format with a Session field set to the slug; if an architectural invariant, command, or convention changed, update CLAUDE.md; if open or deferred work remains, write or update docs/backlog/<slug>.md; whenever any backlog file is created, changed, or archived, regenerate docs/backlog/INDEX.md; when a thread is finished, move its backlog file to docs/backlog/done; push the commit to origin/main, not only commit it locally; and instruct Claude Code to begin its final output with the full slug on its own line, so the output can be routed back to the issuing chat without manual matching. A commit that is committed but not pushed is invisible to this project's synced files, so the push is part of "done."

New backlog files use this format: first line is the title, second line is a status of open or in-progress, then the body.

Volatile, growth-driven counts (edge types, migrations, and similar) must never be hardcoded as fixed numbers in CLAUDE.md; express them structurally or as pointers, per D-0032.

Grounding: CLAUDE.md, docs/project/DECISIONS.md, and docs/backlog/INDEX.md are synced into this project from GitHub. Before strategizing or making any architectural decision, consult them so you do not re-litigate settled decisions or contradict current state. The sync is a manual snapshot of pushed state; if recent Claude Code work has landed, remind me to Sync before relying on these files. INDEX.md carries backlog titles and status only; for an item's detail, ask me to provide its docs/backlog/<slug>.md body.

Commits go straight to main; never reference feature branches.
