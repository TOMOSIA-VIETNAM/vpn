import type { CSSProperties } from "react";

interface NotificationProps {
  title: string;
  body: string;
  className?: string;
  style?: CSSProperties;
}

/** A macOS notification banner posted by the app (English, as the app shows it). */
export function Notification({ title, body, className, style }: NotificationProps) {
  return (
    <div className={`notification ${className ?? ""}`} lang="en" style={style}>
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img src="/logo.png" width={32} height={32} alt="" className="notification__icon" />
      <div>
        <p className="notification__title">{title}</p>
        <p className="notification__body">{body}</p>
      </div>
    </div>
  );
}
