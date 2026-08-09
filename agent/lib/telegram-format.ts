/**
 * Преобразование вывода LLM (Markdown) в HTML для parse_mode=HTML в Telegram.
 *
 * Модель пишет **жирный** и прочие маркдаун-конструкции, а eve отправляет
 * текст без parse_mode (см. docs/channels/telegram.mdx: «passes no parse_mode,
 * so any Markdown shows up literally»). Здесь безопасное подмножество Markdown
 * переводится в HTML-разметку Telegram, а всё остальное экранируется.
 *
 * Поддерживается: **жирный** / __жирный__, *курсив* / _курсив_,
 * ~~зачёркнутый~~, `инлайн-код`, ```блоки кода``` и [ссылки](url).
 * Непарные/неопознанные символы разметки остаются текстом. Теги создаются
 * только для парных конструкций и никогда не оборачивают код (<code>/<pre>):
 * контент-классы исключают плейсхолдер, поэтому сообщение не отвергается
 * Telegram с ошибкой парсинга сущностей.
 */

const PLACEHOLDER = "\u0000";
const stashToken = (index: number) => `${PLACEHOLDER}${index}${PLACEHOLDER}`;

/**
 * Переводит Markdown-текст модели в HTML, пригодный для parse_mode=HTML.
 */
export function markdownToTelegramHtml(text: string): string {
  const placeholders: string[] = [];
  const stash = (html: string): string => {
    const token = stashToken(placeholders.length);
    placeholders.push(html);
    return token;
  };

  let s = text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

  // Блоки кода ```lang\n...``` прячем от последующей обработки: код не должен
  // обрамляться другими тегами (Telegram запрещает вложенные сущности).
  s = s.replace(/```[A-Za-z0-9_-]*\n([\s\S]*?)```/g, (_match, code: string) => stash(`<pre>${code}</pre>`));
  // Инлайн-код `...`
  s = s.replace(/`([^`\n]+)`/g, (_match, code: string) => stash(`<code>${code}</code>`));

  // Жирный и курсив — сначала парные **/__ (могут содержать вложенный курсив).
  // Контент-классы исключают плейсхолдер, чтобы тег не охватил код.
  s = s.replace(/\*\*([^\u0000*\n]+)\*\*/g, (_match, bold: string) => `<b>${bold}</b>`);
  s = s.replace(/__([^\u0000_\n]+)__/g, (_match, bold: string) => `<b>${bold}</b>`);
  s = s.replace(/\*([^\u0000*\n]+)\*/g, (_match, italic: string) => `<i>${italic}</i>`);
  s = s.replace(/_([^\u0000_\n]+)_/g, (_match, italic: string) => `<i>${italic}</i>`);
  // Зачёркнутый ~~...~~
  s = s.replace(/~~([^\u0000~\n]+)~~/g, (_match, struck: string) => `<s>${struck}</s>`);
  // Ссылки [text](url)
  s = s.replace(/\[([^\u0000\]\n]+)\]\(([^\u0000)\s]+)\)/g, (_match, label: string, url: string) => {
    // < и > уже экранированы выше; вычищаем только то, что ломает атрибут href.
    const href = url.replace(/["']/g, "");
    return `<a href="${href}">${label}</a>`;
  });

  for (const [index, html] of placeholders.entries()) {
    // Стрелочная функция — иначе строка-замена интерпретирует $& / $` / $$ / $'
    // в содержимом кода (см. String.replace).
    s = s.replace(stashToken(index), () => html);
  }
  return s;
}

/**
 * Срезает разметку для фолбэка: когда Telegram отверг HTML-версию, шлём тот
 * же текст без markdown-символов.
 */
export function stripMarkdown(text: string): string {
  return text
    .replace(/```[A-Za-z0-9_-]*\n([\s\S]*?)```/g, (_match, code: string) => code)
    .replace(/`([^`\n]+)`/g, (_match, code: string) => code)
    .replace(/\*\*([^*\n]+)\*\*/g, (_match, bold: string) => bold)
    .replace(/__([^_\n]+)__/g, (_match, bold: string) => bold)
    .replace(/\*([^*\n]+)\*/g, (_match, italic: string) => italic)
    .replace(/_([^_\n]+)_/g, (_match, italic: string) => italic)
    .replace(/~~([^~\n]+)~~/g, (_match, struck: string) => struck)
    .replace(/\[([^\]\n]+)\]\(([^)\s]+)\)/g, (_match, label: string) => label)
    .replace(/[*_~`]/g, "");
}
