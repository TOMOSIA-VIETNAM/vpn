import { appNotifications, demoProfiles, type LinkPhase, type NotificationKind } from "@/config/app-ui";
import { Mark, type MarkStyle } from "./Mark";
import { Notification } from "./Notification";
import { Popover, type PopoverState } from "./Popover";

// The status item image for each phase, as the app sets it (menubar-*.png).
const statusItem: Record<LinkPhase, MarkStyle> = {
  idle: "outline",
  connecting: "outline",
  reconnecting: "badged",
  connected: "solid",
  disconnecting: "outline",
  failed: "badged",
};

const notices = appNotifications(demoProfiles[0]);

export interface DeskState extends PopoverState {
  /** Notification banner showing in the top right corner. */
  notice?: NotificationKind;
}

/**
 * A slice of a Mac desktop: wallpaper, menu bar with the app's status item, the
 * popover open under it and, optionally, one of the app's notifications.
 * Decorative (aria-hidden): the surrounding text describes the state.
 */
export function Desk({ state, year, className }: { state: DeskState; year: number; className?: string }) {
  return (
    <div className={`desk ${className ?? ""}`} aria-hidden="true">
      <div className="desk__menubar">
        <span className="desk__status-item">
          <Mark style={statusItem[state.phase]} size={15} />
        </span>
      </div>
      <div className="desk__notices">
        {state.notice && (
          <Notification key={state.notice} {...notices[state.notice]} className="desk__notice" />
        )}
      </div>
      <Popover phase={state.phase} active={state.active} alert={state.alert} year={year} />
    </div>
  );
}
