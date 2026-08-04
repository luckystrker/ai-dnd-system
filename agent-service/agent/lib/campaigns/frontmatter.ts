/**
 * Мини-сериализация YAML frontmatter без внешних зависимостей.
 * Поддерживаемое подмножество: скаляры (строки в JSON-кавычках, числа,
 * булевы), вложенные объекты по отступам и списки (в т.ч. из объектов).
 * Эмиттер и парсер согласованы между собой; парсер также принимает
 * валидный YAML из этого подмножества, написанный руками.
 */

type Scalar = string | number | boolean;

interface Line {
  indent: number;
  text: string;
}

export function buildDocument(data: Record<string, unknown>, body: string): string {
  const frontmatter = serializeFrontmatter(data);
  const trimmedBody = body.trim();
  return `---\n${frontmatter}\n---\n\n${trimmedBody}\n`;
}

export function splitFrontmatter(doc: string): {
  data: Record<string, unknown>;
  body: string;
} {
  const lines = doc.split("\n");
  if (lines[0]?.trim() !== "---") {
    return { data: {}, body: doc.trim() };
  }
  const closing = lines.findIndex((line, index) => index > 0 && line.trim() === "---");
  if (closing === -1) {
    return { data: {}, body: doc.trim() };
  }
  const raw = lines.slice(1, closing).join("\n");
  const body = lines.slice(closing + 1).join("\n").trim();
  return { data: parseFrontmatter(raw), body };
}

export function serializeFrontmatter(data: Record<string, unknown>): string {
  const lines: string[] = [];
  emitMap(data, 0, lines);
  return lines.join("\n");
}

function scalarToYaml(value: Scalar): string {
  if (typeof value === "string") return JSON.stringify(value);
  return String(value);
}

function emitMap(obj: Record<string, unknown>, indent: number, lines: string[]): void {
  const pad = " ".repeat(indent);
  for (const [key, value] of Object.entries(obj)) {
    if (value === undefined || value === null) continue;
    if (Array.isArray(value)) {
      if (value.length === 0) {
        lines.push(`${pad}${key}: []`);
        continue;
      }
      lines.push(`${pad}${key}:`);
      const itemPad = " ".repeat(indent + 2);
      for (const item of value) {
        if (item !== null && typeof item === "object") {
          lines.push(`${itemPad}-`);
          emitMap(item as Record<string, unknown>, indent + 4, lines);
        } else {
          lines.push(`${itemPad}- ${scalarToYaml(item as Scalar)}`);
        }
      }
    } else if (typeof value === "object") {
      lines.push(`${pad}${key}:`);
      emitMap(value as Record<string, unknown>, indent + 2, lines);
    } else {
      lines.push(`${pad}${key}: ${scalarToYaml(value as Scalar)}`);
    }
  }
}

export function parseFrontmatter(raw: string): Record<string, unknown> {
  const lines: Line[] = [];
  for (const source of raw.split("\n")) {
    const match = /^(\s*)(.*)$/.exec(source);
    if (!match) continue;
    const text = match[2];
    if (text.trim() === "" || text.trim().startsWith("#")) continue;
    lines.push({ indent: match[1].replace(/\t/g, "  ").length, text });
  }
  if (lines.length === 0) return {};
  const [value] = parseBlock(lines, 0, lines[0].indent);
  return (value as Record<string, unknown>) ?? {};
}

function parseBlock(lines: Line[], pos: number, indent: number): [unknown, number] {
  if (lines[pos].text === "-" || lines[pos].text.startsWith("- ")) {
    return parseArray(lines, pos, indent);
  }
  return parseMap(lines, pos, indent);
}

function parseMap(lines: Line[], pos: number, indent: number): [Record<string, unknown>, number] {
  const result: Record<string, unknown> = {};
  while (pos < lines.length && lines[pos].indent === indent && !lines[pos].text.startsWith("-")) {
    const { text } = lines[pos];
    const separator = text.indexOf(":");
    if (separator === -1) {
      pos += 1;
      continue;
    }
    const key = parseScalar(text.slice(0, separator).trim()) as string;
    const rest = text.slice(separator + 1).trim();
    pos += 1;
    if (rest === "") {
      const next = lines[pos];
      // Вложенный блок глубже; либо список с дефисом на уровне ключа (допустимый YAML).
      if (
        next &&
        (next.indent > indent || (next.indent === indent && next.text.startsWith("-")))
      ) {
        const [value, after] = parseBlock(lines, pos, next.indent);
        result[key] = value;
        pos = after;
      } else {
        result[key] = null;
      }
    } else {
      result[key] = parseScalar(rest);
    }
  }
  return [result, pos];
}

function parseArray(lines: Line[], pos: number, indent: number): [unknown[], number] {
  const result: unknown[] = [];
  while (pos < lines.length && lines[pos].indent === indent && lines[pos].text.startsWith("-")) {
    const { text } = lines[pos];
    const content = text === "-" ? "" : text.slice(2).trim();
    pos += 1;
    if (content === "") {
      if (pos < lines.length && lines[pos].indent > indent) {
        const [value, next] = parseBlock(lines, pos, lines[pos].indent);
        result.push(value);
        pos = next;
      } else {
        result.push(null);
      }
      continue;
    }
    if (content.includes(":")) {
      // Инлайн-объект: "- key: value", остальные ключи идут глубже.
      const itemLines: Line[] = [{ indent: indent + 2, text: content }];
      while (pos < lines.length && lines[pos].indent > indent) {
        itemLines.push(lines[pos]);
        pos += 1;
      }
      const [value] = parseBlock(itemLines, 0, indent + 2);
      result.push(value);
    } else {
      result.push(parseScalar(content));
    }
  }
  return [result, pos];
}

function parseScalar(text: string): Scalar | null | unknown[] | Record<string, unknown> {
  if (text === "[]") return [];
  if (text === "{}") return {};
  if (text === "null" || text === "~") return null;
  if (text === "true") return true;
  if (text === "false") return false;
  if (text.startsWith('"')) {
    try {
      return JSON.parse(text) as string;
    } catch {
      return text;
    }
  }
  if (/^-?\d+(\.\d+)?$/.test(text)) return Number(text);
  return text;
}
