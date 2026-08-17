/**
 * Единая точка открытия SQLite-базы кампаний (data/campaigns.db) для всех
 * модулей памяти: журнал, NPC и т.п. Схему этот модуль не ведёт — каждый
 * модуль-владелец выполняет свой DDL (CREATE TABLE IF NOT EXISTS) при
 * первом обращении к БД.
 */
import { mkdirSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, resolve } from "node:path";

import type BetterSqlite3 from "better-sqlite3";

/**
 * better-sqlite3 — нативный аддон: бандлер eve не может корректно собрать
 * .node-бинарник (ищет его относительно собственного выходного каталога).
 * Поэтому пакет не импортируется статически, а грузится в рантайме через
 * require из node_modules проекта (идиома из store-sqlite.ts). type-import
 * выше на рантайм не влияет.
 */
const projectRequire = createRequire(resolve(process.cwd(), "package.json"));
let databaseCtor: typeof BetterSqlite3 | undefined;
function betterSqlite(): typeof BetterSqlite3 {
  if (!databaseCtor) {
    databaseCtor = projectRequire("better-sqlite3") as typeof BetterSqlite3;
  }
  return databaseCtor;
}

/** Путь к базе кампаний (переопределяется CAMPAIGN_DB_PATH). */
export function campaignDbPath(): string {
  return resolve(process.cwd(), process.env.CAMPAIGN_DB_PATH ?? "data/campaigns.db");
}

/** Ленивый handle для дефолтного пути: БД открывается при первом обращении. */
let defaultDb: BetterSqlite3.Database | undefined;

function openAt(path: string): BetterSqlite3.Database {
  mkdirSync(dirname(path), { recursive: true });
  const handle = new (betterSqlite())(path);
  handle.pragma("journal_mode = WAL");
  handle.pragma("foreign_keys = ON");
  return handle;
}

/**
 * Открывает SQLite-базу кампаний: WAL, foreign_keys=ON. Лениво — handle
 * создаётся при первом вызове, а не на импорте модуля (eve-снапшот
 * компиляции падает при открытии better-sqlite3 на этапе сборки тулов).
 *
 * Без аргумента возвращается общий кэшированный handle для campaignDbPath()
 * (модули памяти открывают базу только так); с явным путём — свежее
 * соединение без кэша: SqliteCampaignStore сам управляет временем жизни
 * и закрывает его через close().
 */
export function openCampaignDb(): BetterSqlite3.Database;
export function openCampaignDb(dbPath: string): BetterSqlite3.Database;
export function openCampaignDb(dbPath?: string): BetterSqlite3.Database {
  if (dbPath === undefined) {
    if (!defaultDb) defaultDb = openAt(campaignDbPath());
    return defaultDb;
  }
  return openAt(resolve(dbPath));
}
