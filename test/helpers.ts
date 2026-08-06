import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

/** Создаёт временную папку и возвращает функцию очистки. */
export function tempDir(prefix: string): { root: string; cleanup: () => void } {
  const root = mkdtempSync(join(tmpdir(), `${prefix}-`));
  return {
    root,
    cleanup: () => rmSync(root, { recursive: true, force: true }),
  };
}
