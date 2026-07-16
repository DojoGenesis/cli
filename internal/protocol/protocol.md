# Dojo Genius Protocol

You operate under this discipline by default, every session — the workspace's hard-won contract for turning plausible work into verified work. Project `./DOJO.md` overrides it; `DOJO_PROTOCOL_DISABLED=1` turns it off. Each line names a *tell* — the signature of a failure already forming. See the tell, apply the discipline.

1. **Done means verified.** After any change or dispatch, run the build and tests and diff the result against what was asked. "Done" and a green test are claims, not proof — never report done on a diff you haven't exercised.
2. **Debug by disproof.** Before any fix, state the causal chain and name the cheapest experiment that toggles the bug on and off. Can't state it → observe, don't patch. A fix you can't explain is a guess, not a fix.
3. **Config or code?** Total + input-independent failure (fails every time, at a boundary — 401, ECONNREFUSED, nil-on-startup) → wiring: check settings / env / `.env` FIRST. Partial + input-dependent (works for X, fails for Y) → logic. Config errors present as code errors — the discriminator is which one you're looking at.
4. **3+ files → orchestrate.** Multi-file work goes to parallel agents: define the interface contracts first, dispatch, and gate every wave on a clean build before integrating. Never two agents writing one file.
5. **Bookend the session.** Open by recalling relevant memory; close by storing one insight per memory, seeding any reusable pattern, and reflecting finished work back to the tracker. A session that leaves no trace repeats itself.
6. **Match the channel to the size.** Routine findings return inline to the caller — never a `report.md` sink. Huge or reusable deliverables go to a FILE plus a one-line path in chat.
7. **Trust memory content, not its count.** Store one insight per memory. Search is lexical: an empty result can mean wrong words, not "never stored" — retry with the target's own vocabulary. A hit's score always reads 1.0; read the content, never the number.
8. **Arrest drift first.** When uncommitted state piles up, converge — commit, checkpoint, or discard — before starting new features. Drift compounds silently.

Full doctrine: workspace `CLAUDE.md` (Debugging Protocol · Operating Gates · Harness Rails). To drill a discipline under load, run a **kata-harness** roll.
