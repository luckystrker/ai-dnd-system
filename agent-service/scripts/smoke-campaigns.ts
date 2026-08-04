/**
 * Разовый смоук-тест MarkdownCampaignStore: создание кампании, роли,
 * персонажи, привязка к чату и roundtrip frontmatter.
 * Запуск: node scripts/smoke-campaigns.ts
 */
import { rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

process.env.CAMPAIGN_DATA_DIR = join(tmpdir(), `dnd-smoke-${Date.now()}`);

const { MarkdownCampaignStore } = await import("../agent/lib/campaigns/store.ts");
const { StoreError } = await import("../agent/lib/campaigns/types.ts");

const root = process.env.CAMPAIGN_DATA_DIR!;
const store = new MarkdownCampaignStore(root);
let failures = 0;

function check(name: string, fn: () => void) {
  try {
    fn();
    console.log(`ok   ${name}`);
  } catch (error) {
    failures += 1;
    console.error(`FAIL ${name}:`, error);
  }
}

function expectError(name: string, code: string, fn: () => void) {
  try {
    fn();
    failures += 1;
    console.error(`FAIL ${name}: ожидалась ошибка ${code}, но её не было`);
  } catch (error) {
    if (error instanceof StoreError && error.code === code) {
      console.log(`ok   ${name} (${error.message})`);
    } else {
      failures += 1;
      console.error(`FAIL ${name}: неверная ошибка`, error);
    }
  }
}

const dm = { userId: "111", username: "dm_user" };
const player = { userId: "222", username: "player_user" };
const stranger = { userId: "333" };

check("создание кампании", () => {
  const campaign = store.createCampaign(
    {
      title: "Проклятие Ледяной Ведьмы",
      length: "medium",
      setting: "Заснеженное королевство Вальдхейм",
      theme: "Восстание против тирании",
      goal: "Свергнуть Ледяную Ведьму",
      tone: "Мрачный героизм",
      openingScene: "Таверна «Последний очаг» в метель.",
      description: "Длинное описание мира.\nВторая строка.",
    },
    dm,
  );
  if (campaign.slug !== "proklyatie-ledyanoy-vedmy") {
    throw new Error(`неожиданный slug: ${campaign.slug}`);
  }
  if (campaign.members[0].role !== "dm") throw new Error("создатель не dm");
});

const campaign = store.getCampaign("proklyatie-ledyanoy-vedmy")!;
if (!campaign) throw new Error("кампания не найдена по slug");

check("roundtrip frontmatter кампании", () => {
  if (campaign.title !== "Проклятие Ледяной Ведьмы") throw new Error("title потерян");
  if (campaign.goal !== "Свергнуть Ледяную Ведьму") throw new Error("goal потерян");
  if (campaign.openingScene !== "Таверна «Последний очаг» в метель.") throw new Error("openingScene потерян");
  if (campaign.members.length !== 1 || campaign.members[0].userId !== "111") throw new Error("members потеряны");
});

expectError("приглашение не-dm отклоняется", "access_denied", () =>
  store.addMember(campaign.id, stranger.userId, { userId: "444" }),
);

check("dm приглашает игрока", () => {
  const updated = store.addMember(campaign.id, dm.userId, {
    userId: player.userId,
    username: player.username,
  });
  if (updated.members.length !== 2) throw new Error("участник не добавлен");
});

expectError("повторное приглашение отклоняется", "duplicate", () =>
  store.addMember(campaign.id, dm.userId, { userId: player.userId }),
);

expectError("новый dm — только владелец", "access_denied", () => {
  store.addMember(campaign.id, dm.userId, { userId: "555" });
  store.addMember(campaign.id, "555", { userId: "666", role: "dm" });
});

expectError("персонаж от постороннего отклоняется", "access_denied", () =>
  store.saveCharacter(campaign.id, stranger.userId, {
    name: "Торин",
    characterClass: "fighter",
    race: "dwarf",
  }),
);

check("игрок создаёт персонажа", () => {
  const sheet = store.saveCharacter(campaign.id, player.userId, {
    name: "Торин Дубощит",
    characterClass: "fighter",
    race: "dwarf",
    stats: { strength: 16, constitution: 15 },
    background: "Бывший кузнец из разрушенной деревни.",
    motivation: "Вернуть фамильный молот.",
  });
  if (sheet.level !== 1) throw new Error("level по умолчанию != 1");
});

expectError("дубликат имени персонажа отклоняется", "duplicate", () =>
  store.saveCharacter(campaign.id, dm.userId, {
    name: "торин дубощит",
    characterClass: "wizard",
    race: "elf",
  }),
);

check("roundtrip листа персонажа", () => {
  const [sheet] = store.listCharacters(campaign.id);
  if (sheet.name !== "Торин Дубощит") throw new Error("имя потеряно");
  if (sheet.stats.strength !== 16) throw new Error("stats потеряны");
  if (sheet.background !== "Бывший кузнец из разрушенной деревни.") throw new Error("background потерян");
  if (sheet.characterClass !== "fighter") throw new Error("class потерян");
});

expectError("запуск посторонним отклоняется", "access_denied", () =>
  store.bindAndActivate(campaign.id, stranger.userId, { chatId: "-1001" }),
);

check("dm запускает кампанию в чате", () => {
  const active = store.bindAndActivate(campaign.id, dm.userId, { chatId: "-1001", messageThreadId: 7 });
  if (active.status !== "active") throw new Error("статус не active");
});

check("поиск по привязанному чату/топику", () => {
  if (!store.findByBoundChat("-1001", 7)) throw new Error("не найдена по чату+топику");
  if (store.findByBoundChat("-1001", 8)) throw new Error("найден чужой топик");
  if (store.findByBoundChat("-1001")) throw new Error("найден чат без топика");
});

const second = store.createCampaign(
  { title: "Вторая", length: "short", setting: "s", theme: "t" },
  dm,
);

expectError("вторая активная кампания в том же чате отклоняется", "conflict", () =>
  store.bindAndActivate(second.id, dm.userId, { chatId: "-1001", messageThreadId: 7 }),
);

check("повторный запуск той же кампании отклоняется как conflict", () => {
  try {
    store.bindAndActivate(campaign.id, dm.userId, { chatId: "-9999" });
    throw new Error("ожидался conflict");
  } catch (error) {
    if (!(error instanceof StoreError) || error.code !== "conflict") throw error;
  }
});

check("listForUser", () => {
  const mine = store.listForUser(player.userId);
  if (mine.length !== 1 || mine[0].id !== campaign.id) throw new Error("список участника неверен");
});

rmSync(root, { recursive: true, force: true });
if (failures > 0) {
  console.error(`\n${failures} проверок провалено`);
  process.exit(1);
}
console.log("\nВсе проверки пройдены");
