import { stagger } from "./stagger";

interface SplitTextProps {
  text: string;
  /** Animate each word on its own; otherwise the text animates as one piece. */
  byWord: boolean;
  /** Stagger index of the first word, to follow earlier items of the same reveal. */
  start?: number;
}

/**
 * Headline text as stagger items for the reveal animation. Spaces stay real text
 * between the word spans, so copying, line breaking and screen readers are unchanged.
 */
export function SplitText({ text, byWord, start = 0 }: SplitTextProps) {
  if (!byWord) {
    return (
      <span className="split__piece" data-item style={stagger(start)}>
        {text}
      </span>
    );
  }
  let index = start;
  return (
    <>
      {text.split(/(\s+)/).map((part, i) =>
        /^\s+$/.test(part) || part === "" ? (
          part
        ) : (
          <span key={i} className="split__piece" data-item style={stagger(index++)}>
            {part}
          </span>
        ),
      )}
    </>
  );
}

/** Number of stagger items SplitText renders for `text`. */
export function splitCount(text: string, byWord: boolean): number {
  return byWord ? text.split(/\s+/).filter(Boolean).length : 1;
}
