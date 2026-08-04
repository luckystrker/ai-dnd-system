/**
 * Продолжение e2e: от того же пользователя /startcampaign -> привязка к чату.
 * Запуск: node --env-file=.env scripts/e2e-start.ts
 */
import { readFileSync } from "node:fs";
import { join } from "node:path";

const SECRET = process.env.TELEGRAM_WEBHOOK_SECRET_TOKEN;
if (!SECRET) throw new Error("TELEGRAM_WEBHOOK_SECRET_TOKEN is required");
const TARGET = process.env.EVE_URL ?? "http://localhost:2000/eve/v1/telegram";
const CHAT_ID = -7770001;
const USER_ID = 9990001;

const update = {
  update_id: (Date.now() + 1) % 1_000_000_000,
  message: {
    message_id: 2,
    message_thread_id: 42,
    date: Math.floor(Date.now() / 1000),
    from: { id: USER_ID, is_bot: false, first_name: "Smoke", username: "smoke_e2e" },
    chat: { id: CHAT_ID, type: "private" },
    text: "/startcampaign testovyy-pohod — запусти эту кампанию прямо здесь.",
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

const path = join(process.env.CAMPAIGN_DATA_DIR ?? "data/campaigns", "testovyy-pohod", "campaign.md");
const deadline = Date.now() + 180_000;
let doc = "";
while (Date.now() < deadline) {
  doc = readFileSync(path, "utf8");
  if (doc.includes('status: "active"')) break;
  await new Promise((resolve) => setTimeout(resolve, 3000));
}

console.log(`\n--- testovyy-pohod/campaign.md ---\n${doc}`);
const checks: Array<[string, boolean]> = [
  ["status active", doc.includes('status: "active"')],
  ["привязана к чату", doc.includes(`chatId: "${CHAT_ID}"`)],
  ["топик сохранён", doc.includes("messageThreadId: 42")],
];
let failed = 0;
for (const [name, ok] of checks) {
  console.log(`${ok ? "ok  " : "FAIL"} ${name}`);
  if (!ok) failed += 1;
}
process.exit(failed > 0 ? 1 : 0);
