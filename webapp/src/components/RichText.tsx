import { Fragment, type ReactNode } from "react";

/**
 * Renders the inline markup allowed in dictionary strings: **text** becomes
 * <strong>, [[text]] marks a label quoted from the app's English UI (kept on one
 * line), and each {token} is replaced by the matching node in `tokens`.
 */
export function RichText({ text, tokens = {} }: { text: string; tokens?: Record<string, ReactNode> }) {
  const parts = text.split(/(\*\*[^*]+\*\*|\[\[[^\]]+\]\]|\{\w+\})/g);
  return (
    <>
      {parts.map((part, i) => {
        if (part.startsWith("**") && part.endsWith("**")) {
          return <strong key={i}>{part.slice(2, -2)}</strong>;
        }
        if (part.startsWith("[[") && part.endsWith("]]")) {
          return (
            <span key={i} className="ui-label" lang="en">
              {part.slice(2, -2)}
            </span>
          );
        }
        const token = /^\{(\w+)\}$/.exec(part);
        if (token && token[1] in tokens) {
          return <Fragment key={i}>{tokens[token[1]]}</Fragment>;
        }
        return <Fragment key={i}>{part}</Fragment>;
      })}
    </>
  );
}
