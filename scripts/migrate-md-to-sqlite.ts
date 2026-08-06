/**
 * Миграция кампаний из MD-файлов (data/campaigns/) в SQLite: кампании,
 * участники, описания и листы персонажей. Идемпотентна (upsert по id) —
 * можно запускать повторно.
 *
 * Транскрипты, саммари и NPC НЕ мигрируют: они остаются MD-файлами
 * в папках кампаний (journal.ts и npc.ts продолжают работать с ними).
 *
 * Запуск: npm run migrate:sqlite
 */
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";

import { splitFrontmatter } from "../agent/lib/campaigns/frontmatter.ts";
import { MarkdownCampaignStore, campaignDataRoot, campaignDbPath } from "../agent/lib/campaigns/store.ts";
import { SqliteCampaignStore } from "../agent/lib/campaigns/store-sqlite.ts";

const source = new MarkdownCampaignStore(campaignDataRoot());
const target = new SqliteCampaignStore(campaignDbPath());

const campaigns = source.listCampaigns();
let memberCount = 0;
let characterCount = 0;

for (const campaign of campaigns) {
  const mdPath = join(campaignDataRoot(), campaign.slug, "campaign.md");
  const description = existsSync(mdPath)
    ? splitFrontmatter(readFileSync(mdPath, "utf8")).body
    : "";
  target.upsertCampaign(campaign, description);
  memberCount += campaign.members.length;

  for (const sheet of source.listCharacters(campaign.id)) {
    target.upsertCharacter(sheet);
    characterCount += 1;
  }
}

target.close();
console.log(
  `Мигрировано: ${campaigns.length} кампаний, ${memberCount} участников, ` +
    `${characterCount} персонажей -> ${campaignDbPath()}`,
);
