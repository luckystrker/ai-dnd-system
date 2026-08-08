# TTRPG Telegram bot (eve agent)

Eve agent that runs a D&D 5e-style tabletop game in Telegram (solo chat or
group up to 6 players). Bot replies, code comments, commit messages and
`README.md` are in **Russian** — keep that. LLM-facing strings (tool
`description`, `outputSchema` describes) are in English.

## Commands (Node 24 required — `engines`)

```bash
npm run dev -- --no-ui       # local agent
npm run poll                 # 2nd terminal: long-poll -> POST localhost:2000/eve/v1/telegram (auto-resets webhook)
npm run typecheck            # tsc (noEmit)
npm run build                # eve build (bundles; better-sqlite3 stays external — native addon)
npm test                     # node --test "test/*.test.ts" (node:test + TS type-stripping, no build step)
node --test test/store.test.ts   # single test file
npm run smoke                # store round-trip tests (MD + SQLite stores)
npm run migrate:sqlite       # idempotent MD -> SQLite migration
node scripts/dump-sessions.ts [username|chat_id] [--all]   # print chats from .eve/traces (see view-sessions skill)
```

CI (`.github/workflows/ci.yml`) runs: `npm ci` → `typecheck` → `test` → `build`
on Node 24. Tests use temp dirs (`test/helpers.ts`), need no `.env` or DB.

## Architecture

- `agent/agent.ts` — agent entrypoint: OpenRouter-compatible LLM from
  `LLM_BASE_URL`/`LLM_API_KEY`/`LLM_MODEL`; context window scales with campaign
  length (sliding window + eve compaction). `build.externalDependencies`
  must keep `better-sqlite3` external (bundling breaks its `.node` binary).
- `agent/instructions.md` — the DM system prompt; editing it changes bot behavior.
- `agent/tools/` — game tools, auto-discovered by eve. `agent/subagents/chronicler/tools/`
  is a deliberate narrower copy for the chronicler subagent — when adding a tool,
  decide consciously whether the chronicler needs it too.
- Storage split: campaigns/members/characters/quests/threads live in SQLite
  (`data/campaigns.db`, `CAMPAIGN_DB_PATH`; `CAMPAIGN_STORE=markdown` is the
  MD fallback — then quests/threads also move to MD). The always-MD layer, in
  any store, lives in the campaign folder: transcripts (`history/days/`), day
  summaries + headlines, campaign summary, key events, NPC cards
  (`agent/lib/campaigns/journal.ts`/`npc.ts`). Combat state snapshot
  (`combat.md`) is also always-MD, written by `agent/hooks/combat-autosave.ts`
  so an in-progress fight survives a session restart.
- `agent/lib/engine/dnd5e.ts` — the only place dice/check rules live; the LLM
  must quote tool results, never invent rolls.

## Conventions & gotchas

- Env template is `.env.example` (gitignored); `eve dev` loads env files
  automatically, `scripts/telegram-poll.ts` loads `.env` explicitly via
  `--env-file=.env`. README has the full env var table.
- `.eve/` is runtime state (traces, locks, builds) — never commit or rely on it.
  `scripts/dump-sessions.ts` reads traces from there for debugging.
- Group-chat privacy: bot needs privacy mode disabled (BotFather) or admin
  rights, otherwise it never sees player-to-player messages (no transcript,
  no auto-join).
- Single `main` branch; commit messages mix Russian/`feat:`/`fix:` prefixes.

## eve framework

Before writing code, read the relevant guide from the installed eve package
docs (`node_modules/eve/docs/`; fallback: https://eve.dev/docs).

Before implementing an integration yourself, use
`eve registry search <query>` or `eve registry list` to discover available
integrations. Inspect one with `eve registry view <item>`, then install it with
`eve add <item>`.
