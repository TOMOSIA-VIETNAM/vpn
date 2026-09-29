import { screenshots, type ScreenshotKey } from "@/config/site";

interface ScreenshotProps {
  shot: ScreenshotKey;
  alt: string;
  /** Fixed appearance; by default the viewer's (the -dark file under prefers-color-scheme: dark). */
  appearance?: "light" | "dark";
  className?: string;
}

/**
 * An app screenshot. Files are retina captures with the window shadow included,
 * so they render at half their pixel size.
 */
export function Screenshot({ shot, alt, appearance, className }: ScreenshotProps) {
  const { name, width, height } = screenshots[shot];
  const img = (
    // A plain <img> because next/image cannot switch sources by color scheme.
    // eslint-disable-next-line @next/next/no-img-element
    <img
      src={`/screenshots/${name}-${appearance ?? "light"}.png`}
      width={width / 2}
      height={height / 2}
      alt={alt}
      loading="lazy"
      decoding="async"
      className={appearance ? className : undefined}
    />
  );
  if (appearance) return img;
  return (
    <picture className={className}>
      <source media="(prefers-color-scheme: dark)" srcSet={`/screenshots/${name}-dark.png`} />
      {img}
    </picture>
  );
}
