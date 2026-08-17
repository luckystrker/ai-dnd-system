import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { openCampaignDb } from "../agent/lib/campaigns/sqlite-db.ts";

/** Создаёт временную папку и возвращает функцию очистки. */
export function tempDir(prefix: string): { root: string; cleanup: () => void } {
  const root = mkdtempSync(join(tmpdir(), `${prefix}-`));
  return {
    root,
    cleanup: () => rmSync(root, { recursive: true, force: true }),
  };
}

/**
 * Временная SQLite-база кампаний: ставит CAMPAIGN_DB_PATH на файл внутри
 * temp-папки до первого обращения к БД. Возвращает путь и cleanup, который
 * сначала закрывает общий handle (Windows не даёт удалить файл с открытым
 * соединением), затем удаляет папку.
 */
export function tempDb(prefix: string): { path: string; cleanup: () => void } {
  const { root, cleanup } = tempDir(prefix);
  const path = join(root, "campaigns.db");
  process.env.CAMPAIGN_DB_PATH = path;
  return {
    path,
    cleanup: () => {
      try {
        openCampaignDb().close();
      } catch {
        // Handle мог быть закрыт тестом раньше — это нормально.
      }
      cleanup();
    },
  };
}
