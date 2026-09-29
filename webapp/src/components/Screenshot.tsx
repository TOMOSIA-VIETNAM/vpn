import { screenshots, type ScreenshotKey } from "@/config/site";

interface ScreenshotProps {
  shot: ScreenshotKey;
  alt: string;
  className?: string;
  /** Load immediately instead of lazily; use for the screenshot above the fold. */
  priority?: boolean;
}

/**
 * An app screenshot in the viewer's appearance: the -dark variant under
 * prefers-color-scheme: dark, the -light one otherwise. Files are retina
 * captures, so they render at half their pixel size.
 */
export function Screenshot({ shot, alt, className, priority = false }: ScreenshotProps) {
  const { name, width, height } = screenshots[shot];
  return (
    <picture className={className}>
      <source media="(prefers-color-scheme: dark)" srcSet={`/screenshots/${name}-dark.png`} />
      {/* A plain <img> because next/image cannot switch sources by color scheme. */}
      <img
        src={`/screenshots/${name}-light.png`}
        width={width / 2}
        height={height / 2}
        alt={alt}
        loading={priority ? "eager" : "lazy"}
        fetchPriority={priority ? "high" : undefined}
        decoding="async"
      />
    </picture>
  );
}
