function requireEnv(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required`);
  return value;
}

const TOKEN = requireEnv("TELEGRAM_BOT_TOKEN");
const SECRET = requireEnv("TELEGRAM_WEBHOOK_SECRET_TOKEN");
const TARGET = process.env.EVE_URL ?? "http://localhost:2000/eve/v1/telegram";

export {};

async function api(method: string, body: unknown, timeoutMs = 60_000): Promise<unknown> {
  const res = await fetch(`https://api.telegram.org/bot${TOKEN}/${method}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
    signal: AbortSignal.timeout(timeoutMs),
  });
  const data: { ok: boolean; result?: unknown; description?: string } = await res.json();
  if (!data.ok) {
    throw new Error(`Telegram ${method}: ${data.description ?? res.status}`);
  }
  return data.result;
}

async function forward(update: unknown): Promise<void> {
  const res = await fetch(TARGET, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-Telegram-Bot-Api-Secret-Token": SECRET,
    },
    body: JSON.stringify(update),
  });
  if (!res.ok) {
    throw new Error(`eve forward failed: ${res.status} ${await res.text()}`);
  }
}

const isLocal = new URL(TARGET).hostname === "localhost";
if (isLocal) {
  await api("deleteWebhook", {});
  console.log(`[poll] forwarded webhook cleared; polling ${TARGET}`);
} else {
  console.log(`[poll] targeting ${TARGET}; make sure no production webhook is set`);
}

let offset = 0;

while (true) {
  try {
    const updates = (await api("getUpdates", {
      offset,
      timeout: 50,
      allowed_updates: ["message", "callback_query"],
    })) as Array<{ update_id: number }>;
    for (const update of updates) {
      await forward(update);
      offset = update.update_id + 1;
      console.log(`[poll] forwarded update ${update.update_id}`);
    }
  } catch (error) {
    console.error(`[poll] ${error instanceof Error ? error.message : error}`);
    await new Promise((resolve) => setTimeout(resolve, 5000));
  }
}
