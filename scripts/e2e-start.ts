/**
 * Продолжение e2e: от того же пользователя /startcampaign -> привязка к чату.
 * Кампания проверяется через campaignStore (SQLite).
 * Запуск: node --env-file=.env scripts/e2e-start.ts
 */
import { setTimeout as sleep } from "node:timers/promises";

const { campaignStore } = await import("../agent/lib/campaigns/store.ts");

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

// Ждём, пока кампания станет active и привяжется к чату (по БД).
const deadline = Date.now() + 180_000;
let campaign: import("../agent/lib/campaigns/types.ts").Campaign | undefined;
while (Date.now() < deadline) {
  campaign = campaignStore.getCampaign("testovyy-pohod");
  if (campaign?.status === "active" && campaign.boundChat) break;
  await sleep(3000);
}

console.log(`\n--- кампания testovyy-pohod (${campaign?.status ?? "не найдена"}) ---`);
const checks: Array<[string, boolean]> = [
  ["status active", campaign?.status === "active"],
  ["привязана к чату", campaign?.boundChat?.chatId === String(CHAT_ID)],
  ["топик сохранён", campaign?.boundChat?.messageThreadId === 42],
];
let failed = 0;
for (const [name, ok] of checks) {
  console.log(`${ok ? "ok  " : "FAIL"} ${name}`);
  if (!ok) failed += 1;
}
process.exit(failed > 0 ? 1 : 0);
