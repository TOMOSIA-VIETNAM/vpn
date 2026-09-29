"use client";

import { useEffect, useState } from "react";

// How long the button reads "Copied" after a successful copy.
const CONFIRM_MS = 2000;

export function CopyButton({ text, label, copiedLabel }: { text: string; label: string; copiedLabel: string }) {
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), CONFIRM_MS);
    return () => window.clearTimeout(timer);
  }, [copied]);

  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
    } catch {
      // Clipboard access denied (insecure origin or permission): the command stays
      // selectable in the code block, so there is nothing else to do.
    }
  }

  return (
    <button type="button" className="button button--small" onClick={copy} aria-live="polite">
      {copied ? copiedLabel : label}
    </button>
  );
}
