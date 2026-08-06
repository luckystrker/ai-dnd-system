/**
 * Разовый смоук-тест хранилищ кампании. Основной прогон выполняется дважды —
 * для MarkdownCampaignStore и SqliteCampaignStore (кампании, роли, персонажи,
 * привязка к чату, игровые дни, динамическое состояние); поверх — файловые
 * MarkdownNpcStore и журнал (транскрипт/саммари/ключевые события).
 * Запуск: npm run smoke
 */
import { readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

process.env.CAMPAIGN_DATA_DIR = join(tmpdir(), `dnd-smoke-${Date.now()}`);
process.env.CAMPAIGN_DB_PATH = join(tmpdir(), `dnd-smoke-db-${Date.now()}.db`);

const { MarkdownCampaignStore, campaignDbPath, assertCampaignSlug } = await import("../agent/lib/campaigns/store.ts");
const { SqliteCampaignStore } = await import("../agent/lib/campaigns/store-sqlite.ts");
const { MarkdownNpcStore } = await import("../agent/lib/campaigns/npc.ts");
const { StoreError } = await import("../agent/lib/campaigns/types.ts");
const journal = await import("../agent/lib/campaigns/journal.ts");
const { default: Database } = await import("better-sqlite3");

type CampaignStore = import("../agent/lib/campaigns/store.ts").CampaignStore;
type Campaign = import("../agent/lib/campaigns/types.ts").Campaign;

const root = process.env.CAMPAIGN_DATA_DIR!;
const dbPath = campaignDbPath();
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

interface StoreSuiteHooks {
  /** Прямая смена статуса кампании в обход интерфейса (тест фильтра статуса). */
  setStatus(campaignId: string, slug: string, status: string): void;
}

/** Полный прогон проверок CampaignStore; возвращает основную кампанию. */
function runStoreSuite(store: CampaignStore, label: string, hooks: StoreSuiteHooks): Campaign {
  const t = (name: string) => `[${label}] ${name}`;

  check(t("создание кампании"), () => {
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
  if (!campaign) throw new Error(`[${label}] кампания не найдена по slug`);

  check(t("roundtrip кампании"), () => {
    if (campaign.title !== "Проклятие Ледяной Ведьмы") throw new Error("title потерян");
    if (campaign.goal !== "Свергнуть Ледяную Ведьму") throw new Error("goal потерян");
    if (campaign.openingScene !== "Таверна «Последний очаг» в метель.") throw new Error("openingScene потерян");
    if (campaign.members.length !== 1 || campaign.members[0].userId !== "111") throw new Error("members потеряны");
  });

  expectError(t("приглашение не-dm отклоняется"), "access_denied", () =>
    store.addMember(campaign.id, stranger.userId, { userId: "444" }),
  );

  check(t("dm приглашает игрока"), () => {
    const updated = store.addMember(campaign.id, dm.userId, {
      userId: player.userId,
      username: player.username,
    });
    if (updated.members.length !== 2) throw new Error("участник не добавлен");
  });

  expectError(t("повторное приглашение отклоняется"), "duplicate", () =>
    store.addMember(campaign.id, dm.userId, { userId: player.userId }),
  );

  expectError(t("новый dm — только владелец"), "access_denied", () => {
    store.addMember(campaign.id, dm.userId, { userId: "555" });
    store.addMember(campaign.id, "555", { userId: "666", role: "dm" });
  });

  expectError(t("персонаж от постороннего отклоняется"), "access_denied", () =>
    store.saveCharacter(campaign.id, stranger.userId, {
      name: "Торин",
      characterClass: "fighter",
      race: "dwarf",
    }),
  );

  check(t("игрок создаёт персонажа"), () => {
    const sheet = store.saveCharacter(campaign.id, player.userId, {
      name: "Торин Дубощит",
      characterClass: "fighter",
      race: "dwarf",
      stats: { strength: 16, constitution: 15 },
      background: "Бывший кузнец из разрушенной деревни.",
      motivation: "Вернуть фамильный молот.",
      appearance: "stocky dwarf with a braided red beard, scarred cheek, worn plate armor",
    });
    if (sheet.level !== 1) throw new Error("level по умолчанию != 1");
  });

  expectError(t("дубликат имени персонажа отклоняется"), "duplicate", () =>
    store.saveCharacter(campaign.id, dm.userId, {
      name: "торин дубощит",
      characterClass: "wizard",
      race: "elf",
    }),
  );

  check(t("roundtrip листа персонажа"), () => {
    const [sheet] = store.listCharacters(campaign.id);
    if (sheet.name !== "Торин Дубощит") throw new Error("имя потеряно");
    if (sheet.stats.strength !== 16) throw new Error("stats потеряны");
    if (sheet.background !== "Бывший кузнец из разрушенной деревни.") throw new Error("background потерян");
    if (sheet.characterClass !== "fighter") throw new Error("class потерян");
    if (sheet.appearance !== "stocky dwarf with a braided red beard, scarred cheek, worn plate armor") {
      throw new Error("appearance потерян");
    }
    if (sheet.ownerUserId !== player.userId) throw new Error("ownerUserId потерян");
  });

  expectError(t("запуск посторонним отклоняется"), "access_denied", () =>
    store.bindAndActivate(campaign.id, stranger.userId, { chatId: "-1001" }),
  );

  check(t("dm запускает кампанию в чате"), () => {
    const active = store.bindAndActivate(campaign.id, dm.userId, { chatId: "-1001", messageThreadId: 7 });
    if (active.status !== "active") throw new Error("статус не active");
  });

  check(t("поиск по привязанному чату/топику"), () => {
    if (!store.findByBoundChat("-1001", 7)) throw new Error("не найдена по чату+топику");
    if (store.findByBoundChat("-1001", 8)) throw new Error("найден чужой топик");
    if (store.findByBoundChat("-1001")) throw new Error("найден чат без топика");
  });

  const second = store.createCampaign(
    { title: "Вторая", length: "short", setting: "s", theme: "t" },
    dm,
  );

  expectError(t("вторая активная кампания в том же чате отклоняется"), "conflict", () =>
    store.bindAndActivate(second.id, dm.userId, { chatId: "-1001", messageThreadId: 7 }),
  );

  check(t("повторный запуск той же кампании отклоняется как conflict"), () => {
    try {
      store.bindAndActivate(campaign.id, dm.userId, { chatId: "-9999" });
      throw new Error("ожидался conflict");
    } catch (error) {
      if (!(error instanceof StoreError) || error.code !== "conflict") throw error;
    }
  });

  check(t("listForUser"), () => {
    const mine = store.listForUser(player.userId);
    if (mine.length !== 1 || mine[0].id !== campaign.id) throw new Error("список участника неверен");
  });

  check(t("активация выставляет currentDay = 1"), () => {
    const active = store.getCampaign(campaign.id)!;
    if (active.currentDay !== 1) throw new Error(`currentDay = ${active.currentDay}, ожидался 1`);
  });

  // --- Игровые дни ---

  expectError(t("advance_day от игрока отклоняется"), "access_denied", () =>
    store.advanceDay(campaign.id, player.userId),
  );

  check(t("advance_day двигает currentDay"), () => {
    const updated = store.advanceDay(campaign.id, dm.userId);
    if (updated.currentDay !== 2) throw new Error(`currentDay = ${updated.currentDay}, ожидался 2`);
    const reread = store.getCampaign(campaign.id)!;
    if (reread.currentDay !== 2) throw new Error("currentDay не пережил roundtrip");
  });

  // --- Вступление (регистрация по /join) и поиск по чату без фильтра статуса ---

  check(t("autoRegister добавляет написавшего как player"), () => {
    const updated = store.autoRegister(campaign.id, { userId: "777", username: "newbie" });
    const member = updated.members.find((entry) => entry.userId === "777");
    if (!member || member.role !== "player") throw new Error("участник не добавлен как player");
  });

  check(t("autoRegister идемпотентен"), () => {
    const updated = store.autoRegister(campaign.id, { userId: "777", username: "newbie" });
    if (updated.members.filter((entry) => entry.userId === "777").length !== 1) {
      throw new Error("появился дубликат участника");
    }
  });

  check(t("autoRegister принудительно снижает роль dm до player"), () => {
    const updated = store.autoRegister(campaign.id, { userId: "888", role: "dm" });
    const member = updated.members.find((entry) => entry.userId === "888");
    if (!member || member.role !== "player") throw new Error("роль не снижена до player");
  });

  check(t("autoRegister молча пропускает при полной партии"), () => {
    // Игроков уже 3: 222, 777, 888. Дополняем до MAX_PARTY = 6.
    for (const id of ["901", "902", "903"]) {
      store.autoRegister(campaign.id, { userId: id });
    }
    const updated = store.autoRegister(campaign.id, { userId: "904" });
    if (updated.members.some((entry) => entry.userId === "904")) throw new Error("лимит MAX_PARTY не сработал");
  });

  check(t("findByBoundChat c anyStatus находит неактивную кампанию"), () => {
    hooks.setStatus(campaign.id, campaign.slug, "finished");
    try {
      if (store.findByBoundChat("-1001", 7)) throw new Error("finished найдена без anyStatus");
      if (!store.findByBoundChat("-1001", 7, { anyStatus: true })) throw new Error("anyStatus не нашёл finished");
    } finally {
      hooks.setStatus(campaign.id, campaign.slug, "active");
    }
  });

  // --- Динамическое состояние персонажа ---

  check(t("update_character патчит состояние"), () => {
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

  check(t("состояние персонажа переживает roundtrip"), () => {
    const [sheet] = store.listCharacters(campaign.id);
    if (sheet.hp !== 8 || sheet.gold !== 25) throw new Error("состояние потеряно после перечитывания");
    if (sheet.conditions?.[0] !== "отравлен") throw new Error("conditions потеряны");
    if (sheet.location !== "Таверна «Последний очаг»") throw new Error("location потерян");
  });

  expectError(t("update_character для неизвестного персонажа"), "not_found", () =>
    store.updateCharacter(campaign.id, "кто-то", { hp: 1 }),
  );

  return campaign;
}

// --- Markdown-стор ---

const store = new MarkdownCampaignStore(root);

function mdSetStatus(slug: string, status: string) {
  const path = join(root, slug, "campaign.md");
  const doc = readFileSync(path, "utf8");
  const updated = doc.replace(/status: "[a-z]+"/, `status: "${status}"`);
  if (updated === doc) throw new Error(`не удалось подменить статус на ${status}`);
  writeFileSync(path, updated);
}

const campaign = runStoreSuite(store, "md", {
  setStatus: (_campaignId, slug, status) => mdSetStatus(slug, status),
});

// --- SQLite-стор ---

const { campaignStore: defaultStore } = await import("../agent/lib/campaigns/store.ts");

// Дефолтный campaignStore (его же использует npc.ts) создан на той же temp-базе:
// прогоняем suite через него, чтобы заодно проверить продакшен-путь выбора стора.
// Если по окружению дефолт — markdown, берём явный SQLite-стор.
const markdownIsDefault = (process.env.CAMPAIGN_STORE ?? "sqlite").trim().toLowerCase() === "markdown";
const sqliteStore: CampaignStore = markdownIsDefault ? new SqliteCampaignStore(dbPath) : defaultStore;

function sqliteSetStatus(campaignId: string, status: string) {
  // Отдельное подключение: WAL допускает конкурентные соединения,
  // а основное мы не закрываем до конца прогона.
  const db = new Database(dbPath);
  try {
    db.prepare("UPDATE campaigns SET status = ? WHERE id = ?").run(status, campaignId);
  } finally {
    db.close();
  }
}

const sqliteCampaign = runStoreSuite(sqliteStore, "sqlite", {
  setStatus: (campaignId, _slug, status) => sqliteSetStatus(campaignId, status),
});

// --- Квесты и открытые нити ---

function runQuestSuite(store: CampaignStore, campaign: Campaign, label: string) {
  const t = (name: string) => `[${label}] ${name}`;

  check(t("создание квеста"), () => {
    const quest = store.createQuest(campaign.id, {
      title: "Амулет для кузнеца",
      objective: "Найти амулет в развалинах Хмурого Холма",
      difficulty: "medium",
      giverNpcSlug: "Бренна Хмурый",
      rewardPlan: { gold: 200, note: "кузнец отдаст долг" },
    });
    if (quest.slug !== "amulet-dlya-kuznetsa") throw new Error(`неожиданный slug: ${quest.slug}`);
    if (quest.status !== "offered") throw new Error(`статус по умолчанию: ${quest.status}`);
    if (quest.createdDay !== 2) throw new Error(`createdDay = ${quest.createdDay}, ожидался 2`);
    if (quest.rewardPlan?.gold !== 200) throw new Error("rewardPlan потерян");
  });

  expectError(t("дубликат названия квеста отклоняется"), "duplicate", () =>
    store.createQuest(campaign.id, {
      title: "амулет для кузнеца",
      objective: "другое",
      difficulty: "easy",
    }),
  );

  check(t("roundtrip квеста и listQuests"), () => {
    const quests = store.listQuests(campaign.id);
    if (quests.length !== 1) throw new Error(`квестов ${quests.length}, ожидался 1`);
    const quest = quests[0];
    if (quest.title !== "Амулет для кузнеца") throw new Error("title потерян");
    if (quest.objective !== "Найти амулет в развалинах Хмурого Холма") throw new Error("objective потерян");
    if (quest.difficulty !== "medium") throw new Error("difficulty потеряна");
    if (quest.giverNpcSlug !== "Бренна Хмурый") throw new Error("giverNpcSlug потерян");
    if (quest.rewardPlan?.note !== "кузнец отдаст долг") throw new Error("note награды потерян");
  });

  check(t("updateQuest меняет статус и дедлайн"), () => {
    const updated = store.updateQuest(campaign.id, "amulet-dlya-kuznetsa", {
      status: "active",
      deadlineDay: 5,
    });
    if (updated.status !== "active") throw new Error("статус не обновился");
    if (updated.deadlineDay !== 5) throw new Error("дедлайн не обновился");
    const reread = store.listQuests(campaign.id)[0];
    if (reread.status !== "active" || reread.deadlineDay !== 5) {
      throw new Error("изменения не пережили roundtrip");
    }
  });

  check(t("updateQuest находит квест по названию без регистра"), () => {
    const updated = store.updateQuest(campaign.id, "АМУЛЕТ ДЛЯ КУЗНЕЦА", { status: "accepted" });
    if (updated.status !== "accepted") throw new Error("поиск по названию не сработал");
  });

  expectError(t("updateQuest для неизвестного квеста"), "not_found", () =>
    store.updateQuest(campaign.id, "кто-то-другой", { status: "active" }),
  );

  expectError(t("updateQuest в completed отклоняется (нужен complete_quest)"), "conflict", () =>
    store.updateQuest(campaign.id, "amulet-dlya-kuznetsa", { status: "completed" }),
  );

  check(t("updateQuest с переименованием в дубликат отклоняется"), () => {
    store.createQuest(campaign.id, {
      title: "Другое задание",
      objective: "Помочь в канализации",
      difficulty: "easy",
    });
    try {
      store.updateQuest(campaign.id, "Другое задание", { title: "Амулет для кузнеца" });
      throw new Error("ожидался duplicate");
    } catch (error) {
      if (!(error instanceof StoreError) || error.code !== "duplicate") throw error;
    }
  });

  check(t("roundtrip полного rewardPlan (xp/gold/items/note)"), () => {
    const quest = store.createQuest(campaign.id, {
      title: "Полная награда",
      objective: "Собрать налоги",
      difficulty: "hard",
      rewardPlan: { xp: 500, gold: 1000, items: ["Кольцо власти", "Зелье лечения"], note: "должность капитана стражи" },
    });
    const reread = store.listQuests(campaign.id).find((entry) => entry.id === quest.id)!;
    if (reread.rewardPlan?.xp !== 500) throw new Error("xp плана потерян");
    if (reread.rewardPlan?.gold !== 1000) throw new Error("gold плана потерян");
    if (reread.rewardPlan?.items?.length !== 2) throw new Error("items плана потеряны");
    if (reread.rewardPlan?.note !== "должность капитана стражи") throw new Error("note плана потерян");
  });

  check(t("пустой rewardPlan после roundtrip становится undefined (MD и SQLite единообразно)"), () => {
    const quest = store.createQuest(campaign.id, {
      title: "Пустая награда",
      objective: "Осмотреть маяк",
      difficulty: "easy",
      rewardPlan: {},
    });
    const reread = store.listQuests(campaign.id).find((entry) => entry.id === quest.id)!;
    if (reread.rewardPlan !== undefined) {
      throw new Error(`rewardPlan = ${JSON.stringify(reread.rewardPlan)}, ожидался undefined`);
    }
  });

  check(t("appendThread создаёт открытую нить"), () => {
    const thread = store.appendThread(campaign.id, {
      text: "Партия пообещала старосте поговорить со стражей",
      kind: "promise",
    });
    if (thread.status !== "open") throw new Error("статус нити не open");
    if (thread.dayOpened !== 2) throw new Error(`dayOpened = ${thread.dayOpened}, ожидался 2`);
    if (thread.kind !== "promise") throw new Error("kind потерян");
  });

  check(t("resolveThread по фрагменту текста"), () => {
    const resolved = store.resolveThread(campaign.id, "пообещала старосте");
    if (resolved.status !== "resolved") throw new Error("статус не resolved");
    if (resolved.dayClosed !== 2) throw new Error("dayClosed не выставлен");
  });

  expectError(t("resolveThread повторно/для закрытой нити"), "not_found", () =>
    store.resolveThread(campaign.id, "пообещала старосте"),
  );

  check(t("listThreads возвращает и закрытые, и открытые"), () => {
    store.appendThread(campaign.id, { text: "Тайна капюшона", kind: "mystery" });
    const threads = store.listThreads(campaign.id);
    if (threads.length !== 2) throw new Error(`нитей ${threads.length}, ожидалось 2`);
    const closed = threads.find((entry) => entry.status === "resolved");
    if (!closed) throw new Error("закрытая нить не пережила roundtrip");
    const open = threads.find((entry) => entry.kind === "mystery");
    if (!open || open.status !== "open") throw new Error("открытая нить потеряна");
  });

  expectError(t("resolveThread при неоднозначном фрагменте — conflict"), "conflict", () => {
    store.appendThread(campaign.id, { text: "Тайна капюшона и маска", kind: "mystery" });
    store.resolveThread(campaign.id, "капюшон");
  });

  check(t("resolveThread по id закрывает точную нить"), () => {
    const [thread] = store
      .listThreads(campaign.id)
      .filter((entry) => entry.status === "open" && entry.text.includes("и маска"));
    const resolved = store.resolveThread(campaign.id, thread.id);
    if (resolved.status !== "resolved") throw new Error("нить по id не закрылась");
  });

  expectError(t("resolveThread для неизвестной нити"), "not_found", () =>
    store.resolveThread(campaign.id, "не существующая нить"),
  );
}

runQuestSuite(store, campaign, "md");
runQuestSuite(sqliteStore, sqliteCampaign, "sqlite");

// --- Защита slug от path traversal ---

check("assertCampaignSlug отклоняет path traversal", () => {
  const bad = ["../evil", "..", "a/b", "a b", "a..b", "-abc", "abc-", "A", ""];
  for (const slug of bad) {
    try {
      assertCampaignSlug(slug);
      throw new Error(`slug «${slug}» должен быть отклонён`);
    } catch (error) {
      if (!(error instanceof StoreError)) throw error;
    }
  }
  assertCampaignSlug("proklyatie-ledyanoy-vedmy");
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

check("ensureDay создаёт файл нового дня", () => {
  journal.ensureDay(campaign.slug, 2);
  if (!journal.listDays(campaign.slug).includes(2)) throw new Error("файл дня 2 не создан");
});

// --- Промпт иллюстраций сцен ---

const scenePrompt = await import("../agent/lib/scene-prompt.ts");

check("buildScenePrompt собирает сцену, внешность и сеттинг", () => {
  const prompt = scenePrompt.buildScenePrompt({
    sceneDescription: "A tavern at night in a snowstorm",
    appearances: ["stocky dwarf with a braided red beard"],
    setting: "Заснеженное королевство Вальдхейм",
    theme: "Восстание против тирании",
  });
  if (!prompt.startsWith("A tavern at night in a snowstorm")) throw new Error("описание сцены не первое");
  if (!prompt.includes("stocky dwarf with a braided red beard")) throw new Error("внешность не попала в промпт");
  if (!prompt.includes("Setting: Заснеженное королевство Вальдхейм")) throw new Error("сеттинг не попал в промпт");
  if (!prompt.includes(scenePrompt.STYLE_SUFFIX)) throw new Error("суффикс стиля не попал в промпт");
});

check("appearancesForCharacters: case-insensitive, неизвестные имена игнорируются", () => {
  const sheets = store.listCharacters(campaign.id);
  const appearances = scenePrompt.appearancesForCharacters(["торин дубощит", "Кто-То"], sheets);
  if (appearances.length !== 1) throw new Error(`ожидалась 1 внешность, получено ${appearances.length}`);
  if (!appearances[0].includes("braided red beard")) throw new Error("не та внешность");
  if (scenePrompt.appearancesForCharacters(undefined, sheets).length !== 0) throw new Error("undefined должен давать пустой список");
});

// --- NPC ---

// npc.ts резолвит кампании через дефолтный campaignStore (теперь SQLite),
// поэтому NPC-проверки идут по кампании из SQLite-прогона.
const npcStore = new MarkdownNpcStore(root);

check("создание NPC с отношениями и памятью", () => {
  const npc = npcStore.upsertNpc(sqliteCampaign.id, {
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
  const npc = npcStore.getNpc(sqliteCampaign.id, "бренна хмурый");
  if (!npc) throw new Error("NPC не найден по имени без учёта регистра");
  if (npc.role !== "хозяйка таверны") throw new Error("role потерян");
  const relation = npc.relationships["Торин Дубощит"];
  if (!relation || relation.attitude !== 2 || relation.notes !== "налила бесплатно") {
    throw new Error("relationships потеряны");
  }
});

check("обновление NPC дописывает память, не затирая", () => {
  const npc = npcStore.upsertNpc(sqliteCampaign.id, {
    name: "Бренна Хмурый",
    status: "unknown",
    memoryAppend: "Исчезла после ночёвки партии.",
    memoryAppendDay: 2,
  });
  if (!npc.memory.includes("сборщиков налогов")) throw new Error("старая память потеряна");
  if (!npc.memory.includes("Исчезла после ночёвки")) throw new Error("новая память не дописана");
  if (npc.status !== "unknown") throw new Error("status не обновился");
  if (npcStore.listNpcs(sqliteCampaign.id).length !== 1) throw new Error("listNpcs вернул лишнее");
});

// --- Таблицы наград и уровня ---

const rewards = await import("../agent/lib/rewards.ts");

check("levelForXp / xpForLevel согласованы", () => {
  if (rewards.xpForLevel(1) !== 0) throw new Error("порог 1 уровня != 0");
  if (rewards.xpForLevel(2) !== 300) throw new Error("порог 2 уровня != 300");
  if (rewards.levelForXp(0) !== 1) throw new Error("0 XP должен давать 1 уровень");
  if (rewards.levelForXp(299) !== 1) throw new Error("299 XP должен давать 1 уровень");
  if (rewards.levelForXp(300) !== 2) throw new Error("300 XP должен давать 2 уровень");
  if (rewards.levelForXp(1_000_000) !== 20) throw new Error("максимум 20 уровень");
});

check("questXpPerCharacter по сложности и уровню партии", () => {
  // 1 уровень: до 2-го — 300 XP. medium = 25% = 75.
  if (rewards.questXpPerCharacter("medium", 1) !== 75) {
    throw new Error(`medium/1: ${rewards.questXpPerCharacter("medium", 1)}`);
  }
  if (rewards.questXpPerCharacter("easy", 1) !== 36) {
    throw new Error(`easy/1: ${rewards.questXpPerCharacter("easy", 1)}`);
  }
  if (rewards.questXpPerCharacter("hard", 1) !== 120) {
    throw new Error(`hard/1: ${rewards.questXpPerCharacter("hard", 1)}`);
  }
  // 3 уровень: до 4-го — 1800 XP. medium = 25% = 450.
  if (rewards.questXpPerCharacter("medium", 3) !== 450) {
    throw new Error(`medium/3: ${rewards.questXpPerCharacter("medium", 3)}`);
  }
  if (rewards.questXpPerCharacter("medium", 0) !== rewards.questXpPerCharacter("medium", 1)) {
    throw new Error("уровень ниже 1 должен клампиться к 1");
  }
});

check("questGoldRange растёт с уровнем, middleOf — середина", () => {
  const easy = rewards.questGoldRange("easy", 1);
  if (easy[0] !== 25 || easy[1] !== 75) throw new Error(`easy/1: ${JSON.stringify(easy)}`);
  const medium3 = rewards.questGoldRange("medium", 3);
  if (medium3[0] !== 150 || medium3[1] !== 450) throw new Error(`medium/3: ${JSON.stringify(medium3)}`);
  if (rewards.middleOf([100, 300]) !== 200) throw new Error("middleOf неверен");
});

check("classHitDie: англ., русские классы и неизвестные", () => {
  if (rewards.classHitDie("fighter") !== 10) throw new Error("fighter != d10");
  if (rewards.classHitDie("Варвар") !== 12) throw new Error("Варвар != d12");
  if (rewards.classHitDie("волшебник") !== 6) throw new Error("волшебник != d6");
  if (rewards.classHitDie("something") !== 8) throw new Error("неизвестный класс должен давать d8");
});

check("statModifier: floor((stat-10)/2)", () => {
  if (rewards.statModifier(10) !== 0) throw new Error("10 -> 0");
  if (rewards.statModifier(15) !== 2) throw new Error("15 -> 2");
  if (rewards.statModifier(8) !== -1) throw new Error("8 -> -1");
});

// --- Движок d20: маппинг навыков, характеристики, натуральные 20/1 ---

const engine = await import("../agent/lib/engine/dnd5e.ts");

check("resolveSkillAbility: английские и русские названия навыков", () => {
  const cases: Array<[string, string]> = [
    ["perception", "wis"],
    ["Восприятие (Perception)", "wis"],
    ["восприятие", "wis"],
    ["расследование", "int"],
    ["Выживание (Survival)", "wis"],
    ["ловкость рук", "dex"],
    ["sleight of hand", "dex"],
    ["уход за животными", "wis"],
    ["убеждение", "cha"],
  ];
  for (const [skill, expected] of cases) {
    const actual = engine.resolveSkillAbility(skill);
    if (actual !== expected) throw new Error(`"${skill}" -> ${actual}, ожидался ${expected}`);
  }
});

check("skillCheck: модификатор из полных названий характеристик листа", () => {
  // Детерминированный бросок: random()=0.5 -> d20 = floor(0.5*20)+1 = 11.
  const result = engine.skillCheck({ dexterity: 15, intelligence: 8 }, "stealth", 12, null, () => 0.5);
  if (result.ability !== "dex") throw new Error(`ability = ${result.ability}, ожидался dex`);
  if (result.modifier !== 2) throw new Error(`modifier = ${result.modifier}, ожидался +2 (DEX 15)`);
  if (result.roll !== 11 || result.total !== 13 || !result.success) throw new Error(`13 vs DC 12 должен быть успехом: ${JSON.stringify(result)}`);
});

check("skillCheck: проверка на русском берёт верную характеристику", () => {
  // ИНТ 8 -> -1; 11 + (-1) = 10 vs DC 13 -> провал.
  const result = engine.skillCheck({ intelligence: 8 }, "Расследование (Investigation)", 13, null, () => 0.5);
  if (result.ability !== "int") throw new Error(`ability = ${result.ability}, ожидался int`);
  if (result.modifier !== -1) throw new Error(`modifier = ${result.modifier}, ожидался -1 (INT 8)`);
  if (result.success) throw new Error("10 vs DC 13 должен быть провалом");
});

check("skillCheck: натуральная 20 — успех всегда", () => {
  // random()=0.9999 -> d20 = 20, даже против DC 30 и с отрицательным модификатором.
  const result = engine.skillCheck({ wisdom: 5 }, "perception", 30, null, () => 0.9999);
  if (result.roll !== 20) throw new Error(`roll = ${result.roll}, ожидалась 20`);
  if (!result.success || !result.naturalSuccess) throw new Error("натуральная 20 обязана быть успехом");
});

check("skillCheck: натуральная 1 — провал всегда", () => {
  // random()=0 -> d20 = 1, даже с модификатором +5 против DC 1.
  const result = engine.skillCheck({ strength: 20 }, "athletics", 1, null, () => 0);
  if (result.roll !== 1) throw new Error(`roll = ${result.roll}, ожидалась 1`);
  if (result.success || !result.naturalFailure) throw new Error("натуральная 1 обязана быть провалом");
});

// Закрываем SQLite-подключение перед удалением файлов (у прокси close
// делегируется в реальный стор; у Markdown-стора его нет).
const maybeClose = (sqliteStore as { close?: () => void }).close;
if (typeof maybeClose === "function") maybeClose.call(sqliteStore);
rmSync(root, { recursive: true, force: true });
for (const suffix of ["", "-wal", "-shm"]) {
  rmSync(dbPath + suffix, { force: true });
}
if (failures > 0) {
  console.error(`\n${failures} проверок провалено`);
  process.exit(1);
}
console.log("\nВсе проверки пройдены");
