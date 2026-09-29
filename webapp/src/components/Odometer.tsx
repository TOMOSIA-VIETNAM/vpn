import type { CSSProperties } from "react";

const DIGITS = [0, 1, 2, 3, 4, 5, 6, 7, 8, 9];

/**
 * A whole number drawn as odometer wheels. Each digit is a strip of 0–9 then 0 up
 * to the digit; the strip rests on the last entry, so the number is final on the
 * server render, with reduced motion and without JavaScript. Inside a pending
 * [data-reveal] block the strips wait on 0 and roll up once revealed (globals.css).
 * The wheels are hidden from screen readers, which read the number once.
 */
export function Odometer({ value }: { value: number }) {
  const digits = String(value).split("").map(Number);
  return (
    <span className="odometer">
      <span className="odometer__wheels" aria-hidden="true">
        {digits.map((digit, i) => {
          const strip = [...DIGITS, ...DIGITS.slice(0, digit + 1)];
          return (
            <span key={i} className="odometer__wheel">
              <span className="odometer__strip" style={{ "--to": strip.length - 1 } as CSSProperties}>
                {strip.map((d, j) => (
                  <span key={j}>{d}</span>
                ))}
              </span>
            </span>
          );
        })}
      </span>
      <span className="visually-hidden">{value}</span>
    </span>
  );
}
