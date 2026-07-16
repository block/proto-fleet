import { Fragment, type ReactNode } from "react";

type Alignment = "left" | "center" | "right";

type TextBlock = {
  kind: "text";
  content: string;
};

type TableBlock = {
  kind: "table";
  headers: string[];
  rows: string[][];
  alignments: Alignment[];
};

type ContentBlock = TextBlock | TableBlock;

const TABLE_DIVIDER = /^:?-{3,}:?$/;
const NUMERIC_VALUE = /^[-+]?(?:\d{1,3}(?:,\d{3})*|\d+)(?:\.\d+)?(?:\s*(?:%|[A-Za-z]+(?:\/[A-Za-z]+)?))?$/;

const parseTableRow = (line: string): string[] | null => {
  const trimmed = line.trim();
  if (!trimmed.includes("|")) return null;

  const row = trimmed.replace(/^\|/, "").replace(/\|$/, "");
  const cells: string[] = [];
  let cell = "";
  let escaped = false;

  for (const character of row) {
    if (escaped) {
      cell += character === "|" ? "|" : `\\${character}`;
      escaped = false;
    } else if (character === "\\") {
      escaped = true;
    } else if (character === "|") {
      cells.push(cell.trim());
      cell = "";
    } else {
      cell += character;
    }
  }
  if (escaped) cell += "\\";
  cells.push(cell.trim());

  return cells.length >= 2 ? cells : null;
};

const dividerAlignment = (divider: string): Alignment => {
  if (divider.startsWith(":") && divider.endsWith(":")) return "center";
  if (divider.endsWith(":")) return "right";
  return "left";
};

const parseContent = (content: string): ContentBlock[] => {
  const lines = content.replace(/\r\n/g, "\n").split("\n");
  const blocks: ContentBlock[] = [];
  let textLines: string[] = [];

  const flushText = () => {
    const text = textLines.join("\n").trim();
    if (text) blocks.push({ kind: "text", content: text });
    textLines = [];
  };

  for (let lineIndex = 0; lineIndex < lines.length;) {
    const headers = parseTableRow(lines[lineIndex]);
    const dividers = lineIndex + 1 < lines.length ? parseTableRow(lines[lineIndex + 1]) : null;
    const isTable =
      headers !== null &&
      dividers !== null &&
      headers.length === dividers.length &&
      dividers.every((divider) => TABLE_DIVIDER.test(divider));

    if (!isTable || !headers || !dividers) {
      textLines.push(lines[lineIndex]);
      lineIndex += 1;
      continue;
    }

    flushText();
    const rows: string[][] = [];
    lineIndex += 2;
    while (lineIndex < lines.length) {
      const row = parseTableRow(lines[lineIndex]);
      if (!row || row.length !== headers.length) break;
      rows.push(row);
      lineIndex += 1;
    }

    const alignments = dividers.map((divider, columnIndex) => {
      const explicitAlignment = dividerAlignment(divider);
      if (explicitAlignment !== "left") return explicitAlignment;
      const numericColumn = rows.length > 0 && rows.every((row) => NUMERIC_VALUE.test(row[columnIndex]));
      return numericColumn ? "right" : "left";
    });
    blocks.push({ kind: "table", headers, rows, alignments });
  }

  flushText();
  return blocks;
};

const renderInlineText = (content: string): ReactNode =>
  content.split(/(\*\*[^*\n]+\*\*)/g).map((part, index) =>
    part.startsWith("**") && part.endsWith("**") ? (
      <strong key={`${part}-${index}`} className="font-semibold">
        {part.slice(2, -2)}
      </strong>
    ) : (
      <Fragment key={`${part}-${index}`}>{part}</Fragment>
    ),
  );

const alignmentClass = (alignment: Alignment) => {
  if (alignment === "right") return "text-right tabular-nums";
  if (alignment === "center") return "text-center";
  return "text-left";
};

interface ChatMessageContentProps {
  content: string;
}

const ChatMessageContent = ({ content }: ChatMessageContentProps) => {
  const blocks = parseContent(content);

  return (
    <div className="flex min-w-0 flex-col gap-3">
      {blocks.map((block, blockIndex) =>
        block.kind === "text" ? (
          <div key={`text-${blockIndex}`} className="break-words whitespace-pre-wrap">
            {renderInlineText(block.content)}
          </div>
        ) : (
          <div
            key={`table-${blockIndex}`}
            className="max-w-full overflow-x-auto rounded-xl border border-border-5 bg-surface-base"
          >
            <table className="w-full min-w-max border-collapse text-200">
              <thead className="bg-core-primary-5">
                <tr>
                  {block.headers.map((header, columnIndex) => (
                    <th
                      key={`${header}-${columnIndex}`}
                      scope="col"
                      className={`border-b border-border-5 px-3 py-2 text-emphasis-200 whitespace-nowrap ${alignmentClass(block.alignments[columnIndex])}`}
                    >
                      {renderInlineText(header)}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {block.rows.map((row, rowIndex) => (
                  <tr key={`${row.join("-")}-${rowIndex}`} className="border-t border-border-5 first:border-t-0">
                    {row.map((cell, columnIndex) => (
                      <td
                        key={`${cell}-${columnIndex}`}
                        className={`px-3 py-2 align-top ${alignmentClass(block.alignments[columnIndex])}`}
                      >
                        {renderInlineText(cell)}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ),
      )}
    </div>
  );
};

export default ChatMessageContent;
