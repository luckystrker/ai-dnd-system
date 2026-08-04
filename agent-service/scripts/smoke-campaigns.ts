/**
 * Разовый смоук-тест хранилищ кампании: MarkdownCampaignStore (кампании,
 * роли, персонажи, привязка к чату, игровые дни, динамическое состояние),
 * MarkdownNpcStore и журнал (транскрипт/саммари/ключевые события).
 * Запуск: npm run smoke
 */
import { rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

process.env.CAMPAIGN_DATA_DIR = join(tmpdir(), `dnd-smoke-${Date.now()}`);

const { MarkdownCampaignStore } = await import("../agent/lib/campaigns/store.ts");
const { MarkdownNpcStore } = await import("../agent/lib/campaigns/npc.ts");
const { StoreError } = await import("../agent/lib/campaigns/types.ts");
const journal = await import("../agent/lib/campaigns/journal.ts");

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

check("активация выставляет currentDay = 1", () => {
  const active = store.getCampaign(campaign.id)!;
  if (active.currentDay !== 1) throw new Error(`currentDay = ${active.currentDay}, ожидался 1`);
});

// --- Журнал: транскрипт игрового дня, дедупликация, саммари ---

check("append записей транскрипта", () => {
  journal.appendTranscriptEntry(campaign.slug, 1, {
    kind: "player",
    author: "player_user",
    text: "Осматриваю таверну",
    eventId: "evt-1",
    at: "2026-01-01T18:04:00Z",
  });
  journal.appendTranscriptEntry(campaign.slug, 1, {
    kind: "dm",
    text: "Вы видите очаг и барную стойку",
    eventId: "evt-2",
  });
  journal.appendTranscriptEntry(campaign.slug, 1, {
    kind: "action",
    text: "Бросок костей: 17",
    eventId: "evt-3",
  });
  const day = journal.readDay(campaign.slug, 1);
  if (!day || day.entries.length !== 3) throw new Error(`записей ${day?.entries.length ?? 0}, ожидалось 3`);
  if (!day.entries[0].includes("@player_user")) throw new Error("автор player не записан");
});

check("дедупликация по eventId (ретраи хуков)", () => {
  journal.appendTranscriptEntry(campaign.slug, 1, {
    kind: "player",
    author: "player_user",
    text: "Осматриваю таверну",
    eventId: "evt-1",
  });
  const day = journal.readDay(campaign.slug, 1);
  if (day!.entries.length !== 3) throw new Error("дубликат записался");
});

check("readDayTail обрезает хвост", () => {
  const tail = journal.readDayTail(campaign.slug, 1, 1);
  if (tail!.entries.length !== 1) throw new Error("tail не обрезан");
  if (!tail!.entries[0].includes("Бросок костей")) throw new Error("tail вернул не последнюю запись");
});

check("listDays и саммари дня", () => {
  const days = journal.listDays(campaign.slug);
  if (days.length !== 1 || days[0] !== 1) throw new Error(`listDays вернул ${JSON.stringify(days)}`);
  journal.setDaySummary(campaign.slug, 1, "Партия прибыла в таверну.");
  const day = journal.readDay(campaign.slug, 1);
  if (day!.summary !== "Партия прибыла в таверну.") throw new Error("саммари дня не сохранилось");
});

check("накопительное саммари кампании (upsert секции дня)", () => {
  journal.upsertCampaignSummaryDay(campaign.slug, 1, "Первая версия саммари.");
  journal.upsertCampaignSummaryDay(campaign.slug, 1, "Итоговая версия саммари.");
  const summary = journal.readCampaignSummary(campaign.slug);
  if (!summary.includes("Итоговая версия")) throw new Error("секция не обновилась");
  if (summary.includes("Первая версия")) throw new Error("старая секция осталась");
  if ((summary.match(/## День 1/g) ?? []).length !== 1) throw new Error("секция задублировалась");
});

check("ключевые события + дедуп", () => {
  journal.appendKeyEvent(campaign.slug, 1, "Торин нашёл фамильный молот.", "key-1");
  journal.appendKeyEvent(campaign.slug, 1, "Торин нашёл фамильный молот.", "key-1");
  const events = journal.readKeyEvents(campaign.slug);
  if ((events.match(/фамильный молот/g) ?? []).length !== 1) throw new Error("ключевое событие задублировалось");
});

// --- Игровые дни ---

expectError("advance_day от игрока отклоняется", "access_denied", () =>
  store.advanceDay(campaign.id, player.userId),
);

check("advance_day двигает currentDay", () => {
  const updated = store.advanceDay(campaign.id, dm.userId);
  if (updated.currentDay !== 2) throw new Error(`currentDay = ${updated.currentDay}, ожидался 2`);
  journal.ensureDay(campaign.slug, 2);
  if (!journal.listDays(campaign.slug).includes(2)) throw new Error("файл дня 2 не создан");
  const reread = store.getCampaign(campaign.id)!;
  if (reread.currentDay !== 2) throw new Error("currentDay не пережил roundtrip");
});

// --- Динамическое состояние персонажа ---

check("update_character патчит состояние", () => {
  const sheet = store.updateCharacter(campaign.id, "торин дубощит", {
    hp: 8,
    maxHp: 12,
    conditions: ["отравлен"],
    inventory: ["фамильный молот", "факел"],
    gold: 25,
    xp: 300,
    location: "Таверна «Последний очаг»",
  });
  if (sheet.hp !== 8 || sheet.maxHp !== 12) throw new Error("hp не обновился");
  if (sheet.inventory?.length !== 2) throw new Error("инвентарь не обновился");
});

check("состояние персонажа переживает roundtrip", () => {
  const [sheet] = store.listCharacters(campaign.id);
  if (sheet.hp !== 8 || sheet.gold !== 25) throw new Error("состояние потеряно после перечитывания");
  if (sheet.conditions?.[0] !== "отравлен") throw new Error("conditions потеряны");
  if (sheet.location !== "Таверна «Последний очаг»") throw new Error("location потерян");
});

expectError("update_character для неизвестного персонажа", "not_found", () =>
  store.updateCharacter(campaign.id, "кто-то", { hp: 1 }),
);

// --- NPC ---

const npcStore = new MarkdownNpcStore(root);

check("создание NPC с отношениями и памятью", () => {
  const npc = npcStore.upsertNpc(campaign.id, {
    name: "Бренна Хмурый",
    role: "хозяйка таверны",
    location: "Таверна «Последний очаг»",
    firstSeenDay: 1,
    lastSeenDay: 1,
    relationships: {
      "Торин Дубощит": { attitude: 2, notes: "налила бесплатно" },
    },
    memoryAppend: "Игроки помогли ей прогнать сборщиков налогов.",
    memoryAppendDay: 1,
  });
  if (npc.status !== "alive") throw new Error("статус по умолчанию не alive");
  if (!npc.memory.includes("сборщиков налогов")) throw new Error("память NPC не записана");
});

check("getNpc по имени и roundtrip профиля", () => {
  const npc = npcStore.getNpc(campaign.id, "бренна хмурый");
  if (!npc) throw new Error("NPC не найден по имени без учёта регистра");
  if (npc.role !== "хозяйка таверны") throw new Error("role потерян");
  const relation = npc.relationships["Торин Дубощит"];
  if (!relation || relation.attitude !== 2 || relation.notes !== "налила бесплатно") {
    throw new Error("relationships потеряны");
  }
});

check("обновление NPC дописывает память, не затирая", () => {
  const npc = npcStore.upsertNpc(campaign.id, {
    name: "Бренна Хмурый",
    status: "unknown",
    memoryAppend: "Исчезла после ночёвки партии.",
    memoryAppendDay: 2,
  });
  if (!npc.memory.includes("сборщиков налогов")) throw new Error("старая память потеряна");
  if (!npc.memory.includes("Исчезла после ночёвки")) throw new Error("новая память не дописана");
  if (npc.status !== "unknown") throw new Error("status не обновился");
  if (npcStore.listNpcs(campaign.id).length !== 1) throw new Error("listNpcs вернул лишнее");
});

rmSync(root, { recursive: true, force: true });
if (failures > 0) {
  console.error(`\n${failures} проверок провалено`);
  process.exit(1);
}
console.log("\nВсе проверки пройдены");
