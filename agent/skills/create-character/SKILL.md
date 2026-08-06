---
description: Create a new player character - pick an existing campaign, interview the player in the campaign's setting, then save the sheet with save_character.
---

# Character creation interview

Use this when the user asks to create a character (e.g. `/newchar`).
A character can only exist inside an existing campaign - never create one
standalone.

## Procedure

1. Call `list_campaigns`. If the user has no campaigns, explain that a campaign
   must be created first (`/newcampaign`) and stop. If there are several, ask
   which one to use. If exactly one, use it.
2. Call `get_game_context` with that campaign id to load its setting, theme and
   the already created characters.
3. Interview the player, 1-2 questions per message:
   - **Name**.
   - **Class** (D&D 5e: fighter, wizard, rogue, cleric, ranger, etc.) — see
     the beginner guidance below.
   - **Race** (human, elf, dwarf, halfling, ...).
   - **Background**: who the character is and where they come from.
   - **Motivation**: what drives them and what they want from the adventure.
   - **Appearance**: a short portrait description (face, hair, clothing,
     distinguishing features). It is used to draw consistent scene
     illustrations, so collect it as a compact English phrase and pass it as
     `appearance` to `save_character`.
4. Every suggestion must fit the campaign's setting and theme. When offering
   options, reference the campaign world. Check the existing party: if a
   concept overlaps heavily with another character, point it out and suggest a
   twist so the party stays diverse.
5. If the player has no preference for stats, use the D&D 5e standard array
   (15, 14, 13, 12, 10, 8) assigned sensibly to the class; do not stall the
   interview over numbers.
6. Generate the **starting equipment** and **1-2 abilities** yourself for the
   chosen class/race/level — see the balance rules below. Do not ask the
   player to pick them from a catalogue.
7. When everything is collected, show a short summary of the sheet, then call
   `save_character` passing `equipment`, `abilities`, `gold`, `maxHp` and
   `hp` together with the rest. After the save, present the **full character
   card** from the tool result: stats, HP, inventory, abilities, gold,
   background, motivation — so the player sees their finished character.

## Starting equipment and abilities (balance rules)

- New character gets **exactly 1-2 abilities** at level 1, always appropriate
  to their class and level.
- **Castes (magic users) at level 1**: only cantrips and 1st-circle spells
  (fire bolt, magic missile, minor illusion, healing word, detect magic, ...).
  No area destruction, no resurrection, no teleportation, no flight, no
  wishes. **Never** give a level-1 caster anything like "Black Hole",
  "Meteor Swarm", "Wish", "Time Stop", "Power Word: Kill" — such effects are
  unavailable before very high levels.
- **Martial classes at level 1**: one signature class feature (e.g. second
  wind for a fighter, sneak attack for a rogue, rage for a barbarian) plus
  their fighting style or tool proficiency.
- Equipment follows the class: a weapon (and shield for tanky classes), armor
  or robes (mage), arcane focus / holy symbol / component pouch, a light
  source (torch or lantern), a backpack with basic supplies (rations, rope,
  waterskin) and 1-2 personal items fitting the background.
- Starting gold: roughly 10-50 depending on class and background (martial
  warriors and nobles richer, penniless background may start near zero).
- `maxHp` by class at level 1 (example ranges): fighter/barbarian ~12-14,
  cleric/ranger ~10-12, rogue ~8-10, wizard/sorcerer ~6-8. Current `hp` equals
  `maxHp` at start.
- Every ability must have a short description of what it does and how it is
  used (damage dice, range, effect). Pass abilities as objects
  `{ name, description }`.

## Beginner guidance and neutrality

- If the player is new to D&D or has no preference, lead with the simple
  classes — Воин (fighter), Варвар (barbarian), Плут (rogue), Жрец (cleric):
  each has one or two core mechanics and little bookkeeping.
- Classes with heavy resource management — Следопыт (ranger), Волшебник
  (wizard), Друид (druid), Колдун (warlock), Бард (bard) — are fine to offer,
  but always add a one-line complexity note (spells, slots, extra rules).
- Present class options neutrally: no "отлично подходит для этой кампании",
  no recommended option, no praising the pick. Confirm the choice with one
  short neutral sentence and continue.
- Give every option in any list (class, race, background, motivation) a
  factual one-line description only; the player decides what fits.

## Rules

- Only campaign members can save a character. If `save_character` rejects with
  an access error, tell the user they need an invite from the campaign DM
  (`/invite`).
- A duplicate name or a full party (6) comes back as a tool error - relay it
  plainly and offer a fix (new name, wait for a slot).
- Keep messages short and Telegram-friendly.
