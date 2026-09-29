"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  appNotifications,
  demoProfiles,
  demoPublicIp,
  popoverHeader,
  popoverLabels,
  profileSubtitle,
  type DemoProfileId,
  type LinkPhase,
  type NotificationKind,
} from "@/config/app-ui";
import { site } from "@/config/site";
import { motionAllowed } from "./motion/motion";
import { Notification } from "./Notification";

interface DemoState {
  active: DemoProfileId | null;
  phase: LinkPhase;
}

interface ShownNotice {
  key: number;
  kind: NotificationKind;
  leaving: boolean;
}

// The final state, rendered on the server and kept for reduced motion.
const CONNECTED: DemoState = { active: "office", phase: "connected" };

// The story played once on load, in ms from the start: the tunnel comes up, drops,
// and the app brings it back on its own.
const STORY: { at: number; state?: DemoState; notice?: NotificationKind; clearNotices?: true }[] = [
  { at: 0, state: { active: null, phase: "idle" } },
  { at: 1900, state: { active: "office", phase: "connecting" } },
  { at: 3400, state: CONNECTED },
  { at: 5700, state: { active: "office", phase: "reconnecting" }, notice: "lost" },
  { at: 7700, state: CONNECTED, notice: "back" },
  { at: 11000, clearNotices: true },
];

// How long the app takes for the steps a visitor triggers with the switches.
const CONNECT_MS = 1400;
const DISCONNECT_MS = 700;
const NOTICE_EXIT_MS = 300;

const profileById = (id: DemoProfileId | null) => demoProfiles.find((p) => p.id === id) ?? null;

/**
 * A working replica of the app's menu bar popover: the switches connect and
 * disconnect, and on load it plays a drop-and-reconnect with the app's notifications.
 */
export function PopoverDemo({ label, year }: { label: string; year: number }) {
  const [state, setState] = useState<DemoState>(CONNECTED);
  const [notices, setNotices] = useState<ShownNotice[]>([]);
  const timers = useRef<number[]>([]);
  const noticeKey = useRef(0);

  const clearTimers = useCallback(() => {
    timers.current.forEach((t) => window.clearTimeout(t));
    timers.current = [];
  }, []);

  const later = useCallback((ms: number, run: () => void) => {
    timers.current.push(window.setTimeout(run, ms));
  }, []);

  const dismissNotices = useCallback(() => {
    setNotices((shown) => shown.map((n) => ({ ...n, leaving: true })));
    later(NOTICE_EXIT_MS, () => setNotices([]));
  }, [later]);

  useEffect(() => {
    if (!motionAllowed()) return;
    for (const step of STORY) {
      later(step.at, () => {
        if (step.state) setState(step.state);
        if (step.notice) {
          const kind = step.notice;
          setNotices((shown) => [{ key: ++noticeKey.current, kind, leaving: false }, ...shown]);
        }
        if (step.clearNotices) dismissNotices();
      });
    }
    return clearTimers;
  }, [later, clearTimers, dismissNotices]);

  function toggle(id: DemoProfileId) {
    clearTimers();
    dismissNotices();
    const turningOff = state.active === id && state.phase !== "disconnecting";
    if (turningOff) {
      setState({ active: id, phase: "disconnecting" });
      later(DISCONNECT_MS, () => setState({ active: null, phase: "idle" }));
    } else {
      setState({ active: id, phase: "connecting" });
      later(CONNECT_MS, () => setState({ active: id, phase: "connected" }));
    }
  }

  const activeProfile = profileById(state.active);
  const header = popoverHeader(state.phase, activeProfile);
  const office = profileById("office")!;
  const messages = appNotifications(office);

  return (
    <>
      <div className="notices" aria-hidden="true">
        {notices.map((n) => (
          <Notification
            key={n.key}
            title={messages[n.kind].title}
            body={messages[n.kind].body}
            className={n.leaving ? "notices__item is-leaving" : "notices__item"}
          />
        ))}
      </div>

      <div className="popover" role="group" aria-label={label} lang="en">
        <div className="popover__header">
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src="/logo.png" width={36} height={36} alt="" className="popover__logo" />
          <div className="popover__heading">
            <p className="popover__name">{site.name}</p>
            <p className="popover__status">
              <span className={`status-dot status-dot--${header.tone}`} aria-hidden="true" />
              <span className="popover__status-word">{header.status}</span>
              <span className="popover__status-detail"> · {header.detail}</span>
            </p>
          </div>
        </div>

        <div className={`popover__tunnel ${state.phase === "connected" ? "is-open" : ""}`}>
          <div className="popover__tunnel-inner">
            <div className="popover__card">
              <p className="popover__card-label">{popoverLabels.publicIp}</p>
              <p className="popover__card-value">{demoPublicIp}</p>
            </div>
          </div>
        </div>

        <div className="popover__section">
          <p className="popover__section-title">
            {popoverLabels.configurations}
            <span aria-hidden="true" className="popover__plus">
              +
            </span>
          </p>
          <ul className="popover__profiles">
            {demoProfiles.map((profile) => {
              const isActive = state.active === profile.id;
              const phase = isActive ? state.phase : null;
              const on = isActive && state.phase !== "disconnecting";
              const tone =
                phase === "connected" ? "ok" : phase && phase !== "idle" ? "warning" : "idle";
              return (
                <li key={profile.id} className="popover__profile">
                  <span
                    className={`status-dot status-dot--${tone} ${tone === "warning" ? "is-pulsing" : ""}`}
                    aria-hidden="true"
                  />
                  <div className="popover__profile-text">
                    <p className="popover__profile-name">{profile.title}</p>
                    <p className="popover__profile-sub">{profileSubtitle(profile, phase)}</p>
                  </div>
                  <button
                    type="button"
                    role="switch"
                    aria-checked={on}
                    aria-label={profile.title}
                    className="switch"
                    onClick={() => toggle(profile.id)}
                  >
                    <span className="switch__knob" />
                  </button>
                  <span className="popover__more" aria-hidden="true">
                    ···
                  </span>
                </li>
              );
            })}
          </ul>
        </div>

        <div className="popover__footer">
          <span>
            © {year} {site.company}
          </span>
          <span className="popover__footer-icons" aria-hidden="true">
            <svg viewBox="0 0 20 20" width="15" height="15">
              <circle cx="10" cy="10" r="2.6" fill="none" stroke="currentColor" strokeWidth="1.5" />
              <path
                d="M10 2.5v2M10 15.5v2M2.5 10h2M15.5 10h2M4.7 4.7l1.4 1.4M13.9 13.9l1.4 1.4M4.7 15.3l1.4-1.4M13.9 6.1l1.4-1.4"
                stroke="currentColor"
                strokeWidth="1.5"
                strokeLinecap="round"
              />
            </svg>
            <svg viewBox="0 0 20 20" width="15" height="15">
              <path d="M10 2.5v7" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
              <path
                d="M6 5.2a6 6 0 1 0 8 0"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.6"
                strokeLinecap="round"
              />
            </svg>
          </span>
        </div>
      </div>
    </>
  );
}
