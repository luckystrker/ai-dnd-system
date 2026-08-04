---
description: Create a new campaign - run the campaign creation interview (length, setting, theme, goal), then save the structure with save_campaign.
---

# Campaign creation interview

Use this when the user asks to create/initialize a campaign (e.g. `/newcampaign`).

## Procedure

1. First check with `list_campaigns` whether the user already has campaigns in
   `setup` status. Mention them briefly so the user can abandon creation and
   start an existing one instead.
2. Interview the user, asking 1-2 questions per message, in this order:
   - **Length**: short (1-3 sessions), medium (4-10 sessions), or long (10+).
   - **Setting**: the world and environment (e.g. classic high fantasy, dark
     gothic, planar odyssey).
   - **Theme / leitmotif**: what the story is really about (e.g. rebellion
     against a tyrant, exploration of ancient ruins, heist against a thieves' guild).
   - **Final goal** (optional): the players' end goal, if they already have one.
     It is fine to leave this open and define it in play.
   - **Tone**: heroic, grim, humorous, mysterious.
   - **Opening scene** (optional): where the adventure begins.
3. Whenever the user skips a question, answers "I don't know", or asks you to
   decide, offer 2-3 concrete options phrased as a clear choice, e.g.
   "Pick one: 1) ..., 2) ..., 3) ... - or suggest your own". Never silently
   skip a question: every field must be either answered by the user or chosen
   from your suggestions.
4. When everything is collected, summarize the campaign in a few lines, then
   call `save_campaign` with the full structure. Put a rich world description
   into `description` - it is stored in the campaign file and used later.
5. After saving, tell the user the campaign is ready in setup status and that
   `/startcampaign` begins it and binds it to the current chat. Characters can
   already be created with `/newchar`.

## Rules

- Keep messages short and Telegram-friendly.
- Default system is D&D 5e; adapt suggestions to high fantasy unless the user
  asks otherwise.
- Never call `save_campaign` with empty required fields.
