/**
 * Разовый e2e-прогон: синтетический Telegram webhook -> eve -> save_campaign.
 * Требует запущенный `npm run dev` и .env с TELEGRAM_WEBHOOK_SECRET_TOKEN.
 * Запуск: node --env-file=.env scripts/e2e-webhook.ts
 */
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

const SECRET = process.env.TELEGRAM_WEBHOOK_SECRET_TOKEN;
if (!SECRET) throw new Error("TELEGRAM_WEBHOOK_SECRET_TOKEN is required");
const TARGET = process.env.EVE_URL ?? "http://localhost:2000/eve/v1/telegram";
const CHAT_ID = -7770001;
const USER_ID = 9990001;

const update = {
  update_id: Date.now() % 1_000_000_000,
  message: {
    message_id: 1,
    message_thread_id: 42,
    date: Math.floor(Date.now() / 1000),
    from: { id: USER_ID, is_bot: false, first_name: "Smoke", username: "smoke_e2e" },
    chat: { id: CHAT_ID, type: "private" },
    text:
      "/newcampaign Создай кампанию без вопросов, сразу сохрани через save_campaign: " +
      "название «Тестовый поход», длина short, сеттинг — приграничье с гоблинами, " +
      "лейтмотив — оборона деревни, цель — прогнать гоблинов, тон — героический, " +
      "стартовая сцена — деревня на рассвете. Затем запусти её здесь через start_campaign.",
  },
};

const res = await fetch(TARGET, {
  method: "POST",
  headers: {
    "Content-Type": "application/json",
    "X-Telegram-Bot-Api-Secret-Token": SECRET,
  },
  body: JSON.stringify(update),
});
console.log(`webhook POST -> ${res.status}`);
if (!res.ok) {
  console.error(await res.text());
  process.exit(1);
}

const dataRoot = process.env.CAMPAIGN_DATA_DIR ?? "data/campaigns";
const deadline = Date.now() + 180_000;
let found: string | undefined;
while (Date.now() < deadline) {
  if (existsSync(dataRoot)) {
    found = readdirSync(dataRoot).find((dir) => dir.includes("testovyy") || dir.includes("test"));
    if (found) break;
  }
  await new Promise((resolve) => setTimeout(resolve, 3000));
}

if (!found) {
  console.error(`FAIL: кампания не появилась в ${dataRoot} за 180 секунд`);
  process.exit(1);
}

const doc = readFileSync(join(dataRoot, found, "campaign.md"), "utf8");
console.log(`\n--- ${found}/campaign.md ---\n${doc}`);
const checks: Array<[string, boolean]> = [
  ["есть frontmatter", doc.startsWith("---")],
  ["создатель = dm", doc.includes('role: "dm"')],
  ["userId сохранён", doc.includes(`userId: "${USER_ID}"`)],
  ["length short", doc.includes('length: "short"')],
  ["status active после start_campaign", doc.includes('status: "active"')],
  ["привязана к чату", doc.includes(`chatId: "${CHAT_ID}"`)],
];
let failed = 0;
for (const [name, ok] of checks) {
  console.log(`${ok ? "ok  " : "FAIL"} ${name}`);
  if (!ok) failed += 1;
}
process.exit(failed > 0 ? 1 : 0);
