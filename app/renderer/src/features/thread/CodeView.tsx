// Highlighted code. It renders plain text until the tokens arrive, so a block
// never waits for the highlighter to appear.

import { useTokens } from "../../lib/highlight";

export function Code({ code, language, delay }: { code: string; language: string; delay?: number }) {
  const tokens = useTokens(code, language, delay);
  if (!tokens) return <code>{code}</code>;
  return (
    <code>
      {tokens.map((line, i) => (
        <span key={i}>
          {line.map((token, j) => (
            <span key={j} style={{ color: token.color, fontStyle: (token.fontStyle ?? 0) & 1 ? "italic" : undefined }}>
              {token.content}
            </span>
          ))}
          {i < tokens.length - 1 && "\n"}
        </span>
      ))}
    </code>
  );
}
