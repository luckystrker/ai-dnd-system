/**
 * Выводит переписку пользователей с ботом из локальных OTLP-трейсов
 * (.eve/traces/v1). По умолчанию показывает только личные чаты (DM).
 *
 * Запуск:
 *   node scripts/dump-sessions.ts                 # все личные чаты
 *   node scripts/dump-sessions.ts vasya           # фильтр по username/имени чата
 *   node scripts/dump-sessions.ts 123456789       # фильтр по user_id / chat_id
 *   node scripts/dump-sessions.ts --all           # все чаты, включая группы
 *
 * Если кириллица в консоли отображается кракозябрами, выполните: chcp 65001
 */
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";

const ROOT = join(import.meta.dirname, "..", ".eve", "traces", "v1");

const args = process.argv.slice(2);
const filter = args.filter((a) => !a.startsWith("--"))[0] ?? "";
const allChats = args.includes("--all");

interface Msg {
  role: "user" | "assistant" | string;
  text: string;
  at: number;
}

interface Session {
  sessionId: string;
  chatId: string;
  chatType: string;
  chatTitle: string;
  userId: string;
  username: string;
  messages: Msg[];
}

function spanText(span: any, key: string): string | undefined {
  for (const a of span.attributes ?? []) {
    if (a.key === key && a.value?.stringValue) {
      return a.value.stringValue;
    }
  }
  return undefined;
}

function parseContext(prompt: string): Record<string, string> {
  const m = prompt.match(/<telegram_context>([\s\S]*?)<\/telegram_context>/);
  if (!m) return {};
  const ctx: Record<string, string> = {};
  for (const line of m[1].split("\n")) {
    const idx = line.indexOf(":");
    if (idx > 0) ctx[line.slice(0, idx).trim()] = line.slice(idx + 1).trim();
  }
  return ctx;
}

function contentToText(content: any): string {
  if (typeof content === "string") return content;
  if (Array.isArray(content)) {
    return content
      .filter((p: any) => p.type === "text")
      .map((p: any) => p.text ?? "")
      .join("\n");
  }
  return "";
}

/** Слить историю всех промптов сессии: каждый следующий промпт — надстройка над
 * предыдущим, но eve компактит контекст (промпт может стать короче) — тогда
 * урезаем накопленную историю до увиденного моделью. */
function buildHistory(frames: any[]): { history: Msg[]; lastResponse: string | undefined } {
  const history: Msg[] = [];
  let lastResponse: string | undefined;
  for (const frame of frames) {
    const msgs = (JSON.parse(frame.prompt) as any[])
      .filter((m: any) => !(typeof m.content === "string" && m.content.startsWith("<telegram_context>")))
      .map((m: any) => ({ role: m.role, text: contentToText(m.content) }))
      .filter((m: any) => m.text);
    let common = 0;
    while (
      common < msgs.length &&
      common < history.length &&
      history[common].role === msgs[common].role &&
      history[common].text === msgs[common].text
    ) {
      common++;
    }
    if (history.length > common) {
      history.length = common;
    }
    for (let i = common; i < msgs.length; i++) {
      history.push({ role: msgs[i].role, text: msgs[i].text, at: frame.timeNs });
    }
    const resp = frame.response;
    if (resp) lastResponse = resp;
  }
  return { history, lastResponse };
}

function collectSessions(): Session[] {
  const tracesRoot = ROOT;
  if (!statSync(tracesRoot, { throwIfNoEntry: false })) {
    console.error(`Нет трейсов: ${tracesRoot}. Запустите eve dev, чтобы сессии записывались.`);
    process.exit(1);
  }

  const bySession = new Map<string, any[]>();
  for (const trace of readdirSync(tracesRoot)) {
    const segDir = join(tracesRoot, trace, "segments");
    if (!statSync(segDir, { throwIfNoEntry: false })) continue;
    for (const file of readdirSync(segDir)) {
      if (!file.endsWith(".otlp.json")) continue;
      let doc: any;
      try {
        doc = JSON.parse(readFileSync(join(segDir, file), "utf8"));
      } catch {
        continue;
      }
      for (const rs of doc.resourceSpans ?? []) {
        for (const ss of rs.scopeSpans ?? []) {
          for (const span of ss.spans ?? []) {
            const prompt = spanText(span, "ai.prompt.messages");
            if (!prompt) continue;
            let msgs: any[];
            try {
              msgs = JSON.parse(prompt);
            } catch {
              continue;
            }
            const ctxMsg = msgs.find(
              (m: any) => typeof m.content === "string" && m.content.startsWith("<telegram_context>"),
            );
            const ctx = ctxMsg ? parseContext(ctxMsg.content) : {};
            const sessionId = spanText(span, "agent.session.id") ?? trace;
            const timeNs = Number(span.startTimeUnixNano);
            const chatId = ctx.chat_id ?? "";
            const frame = {
              prompt,
              response: spanText(span, "ai.response.text"),
              chatId,
              chatType: ctx.chat_type ?? "",
              chatTitle: ctx.chat_title ?? "",
              userId: ctx.user_id ?? "",
              username: ctx.username ?? "",
              timeNs,
            };
            if (!bySession.has(sessionId)) bySession.set(sessionId, []);
            bySession.get(sessionId)!.push(frame);
          }
        }
      }
    }
  }

  const sessions: Session[] = [];
  for (const [sessionId, frames] of bySession) {
    frames.sort((a, b) => a.timeNs - b.timeNs);
    const first = frames[0];
    const { history, lastResponse } = buildHistory(frames);
    if (lastResponse && history.length) {
      const last = history[history.length - 1];
      if (!(last.role === "assistant" && last.text === lastResponse)) {
        history.push({ role: "assistant", text: lastResponse, at: frames[frames.length - 1].timeNs });
      }
    }
    sessions.push({
      sessionId,
      chatId: first.chatId,
      chatType: first.chatType,
      chatTitle: first.chatTitle,
      userId: first.userId,
      username: first.username,
      messages: history,
    });
  }

  sessions.sort((a, b) => {
    const ta = a.messages[0]?.at ?? 0;
    const tb = b.messages[0]?.at ?? 0;
    return tb - ta;
  });
  return sessions;
}

function fmtTime(ns: number): string {
  const d = new Date(ns / 1e6);
  return d.toISOString().slice(0, 16).replace("T", " ");
}

function main() {
  const sessions = collectSessions();
  const matched = sessions.filter((s) => {
    if (!allChats && s.chatType !== "private") return false;
    if (!filter) return true;
    const hay = `${s.username} ${s.userId} ${s.chatId} ${s.chatTitle}`.toLowerCase();
    return hay.includes(filter.toLowerCase());
  });

  if (!matched.length) {
    console.log(`Ничего не найдено.${filter ? ` Фильтр: "${filter}"` : ""}`);
    if (!filter && !allChats) {
      const types = [...new Set(sessions.map((s) => `${s.chatType}`))];
      console.log(`Найдены чаты типов: ${types.join(", ") || "нет"}. Попробуйте --all.`);
    }
    return;
  }

  for (const s of matched) {
    const who = s.username || `user ${s.userId}`;
    const title = s.chatTitle ? ` "${s.chatTitle}"` : "";
    console.log(
      `\n==== ${who}${title} | chat_id ${s.chatId} | ${s.chatType} | session ${s.sessionId.slice(0, 8)} ====`,
    );
    if (!s.messages.length) {
      console.log("  (сообщений нет)");
      continue;
    }
    for (const m of s.messages) {
      const from = m.role === "assistant" ? "бот" : who;
      console.log(`[${fmtTime(m.at)}] ${from}: ${m.text.replace(/\n/g, "\n    ")}`);
    }
  }
}

main();
