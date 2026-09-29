/** Fills `{name}` placeholders, e.g. `format("macOS {version}+", { version: "12" })`. */
export function format(template: string, values: Record<string, string>): string {
  return template.replace(/\{(\w+)\}/g, (match, key: string) => values[key] ?? match);
}
