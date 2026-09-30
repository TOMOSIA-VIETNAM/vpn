import type { AppAlert } from "@/config/app-ui";
import { popoverLabels } from "@/config/app-ui";
import { Icon } from "./Icon";

/**
 * The card the popover shows for a failure (AlertCard in app/): tinted by
 * tone, the app's glyph and title, the message, then the fix and Dismiss. A
 * state without a fix (waiting for the network) has no buttons.
 * English, as the app shows it.
 */
export function AlertCard({ alert, className }: { alert: AppAlert; className?: string }) {
  return (
    <div className={`alert-card alert-card--${alert.tone} ${className ?? ""}`} lang="en">
      <p className="alert-card__title">
        <Icon name={alert.icon} size={14} className="alert-card__icon" />
        {alert.title}
      </p>
      <p className="alert-card__message">{alert.message}</p>
      {alert.action && (
        <p className="alert-card__buttons">
          <span className="alert-card__button alert-card__button--primary">{alert.action}</span>
          <span className="alert-card__button">{popoverLabels.dismiss}</span>
        </p>
      )}
    </div>
  );
}
