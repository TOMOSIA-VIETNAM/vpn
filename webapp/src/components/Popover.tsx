import {
  demoProfiles,
  demoPublicIp,
  popoverHeader,
  popoverLabels,
  profileSubtitle,
  type AppAlert,
  type DemoProfileId,
  type LinkPhase,
} from "@/config/app-ui";
import { site } from "@/config/site";
import { AlertCard } from "./AlertCard";

export interface PopoverState {
  phase: LinkPhase;
  /** The configuration whose switch is on, if any. */
  active: DemoProfileId | null;
  /** Failure card shown under the header (with phase "failed"). */
  alert?: AppAlert;
}

/** Dot tone of a configuration row. */
function rowTone(phase: LinkPhase | null) {
  if (phase === "connected") return "ok";
  if (phase === "connecting" || phase === "reconnecting" || phase === "disconnecting") return "warning";
  return "idle";
}

/** The public IP card the popover opens while connected. */
export function PublicIpCard({ open }: { open: boolean }) {
  return (
    <div className={`popover__tunnel ${open ? "is-open" : ""}`}>
      <div className="popover__tunnel-inner">
        <div className="popover__card">
          <p className="popover__card-label">{popoverLabels.publicIp}</p>
          <p className="popover__card-value">{demoPublicIp}</p>
        </div>
      </div>
    </div>
  );
}

/** The configuration rows, each with its switch. */
export function ProfileList({ phase, active }: Pick<PopoverState, "phase" | "active">) {
  return (
    <ul className="popover__profiles">
      {demoProfiles.map((profile) => {
        const rowPhase = active === profile.id ? phase : null;
        const on = rowPhase !== null && rowPhase !== "disconnecting" && rowPhase !== "failed";
        const tone = rowTone(rowPhase);
        return (
          <li key={profile.id} className="popover__profile">
            <span className={`status-dot status-dot--${tone} ${tone === "warning" ? "is-pulsing" : ""}`} />
            <span className="popover__profile-text">
              <span className="popover__profile-name">{profile.title}</span>
              <span className="popover__profile-sub">{profileSubtitle(profile, rowPhase)}</span>
            </span>
            <span className={on ? "switch is-on" : "switch"}>
              <span className="switch__knob" />
            </span>
          </li>
        );
      })}
    </ul>
  );
}

/**
 * Replica of the app's menu bar popover in a given state, sized like the app at 1x.
 * Decorative: the page text around it says what it shows.
 */
export function Popover({ phase, active, alert, year }: PopoverState & { year: number }) {
  const profile = demoProfiles.find((p) => p.id === active) ?? null;
  const header = popoverHeader(phase, profile);
  return (
    <div className="popover" lang="en" aria-hidden="true">
      <div className="popover__header">
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img src="/logo.png" width={36} height={36} alt="" className="popover__logo" />
        <div>
          <p className="popover__name">{site.name}</p>
          <p className="popover__status">
            <span className={`status-dot status-dot--${header.tone}`} />
            <span className="popover__status-word">{header.status}</span>
            <span className="popover__status-detail">· {header.detail}</span>
          </p>
        </div>
      </div>

      <PublicIpCard open={phase === "connected"} />
      <div className={`popover__tunnel ${alert ? "is-open" : ""}`}>
        <div className="popover__tunnel-inner">{alert && <AlertCard alert={alert} className="popover__alert" />}</div>
      </div>

      <div className="popover__section">
        <p className="popover__section-title">
          {popoverLabels.configurations}
          <span className="popover__plus">+</span>
        </p>
        <ProfileList phase={phase} active={active} />
      </div>

      <div className="popover__footer">
        <span>
          © {year} {site.company}
        </span>
      </div>
    </div>
  );
}
