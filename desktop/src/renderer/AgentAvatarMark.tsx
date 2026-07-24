import type { CSSProperties, JSX } from "react";

export const AGENT_AVATAR_KEYS = [
  "abstract-1",
  "abstract-2",
  "abstract-3",
  "abstract-4",
  "abstract-5",
  "abstract-6",
  "abstract-7",
  "abstract-8",
  "abstract-9",
] as const;

export type AgentAvatarKey = (typeof AGENT_AVATAR_KEYS)[number];

const MOTIFS: JSX.Element[] = [
  <><circle cx="24" cy="24" r="11" /><circle cx="24" cy="24" r="6" fill="var(--agent-avatar-bg)" /></>,
  <><circle cx="24" cy="24" r="11.5" /><circle cx="29.5" cy="19.5" r="9.6" fill="var(--agent-avatar-bg)" /></>,
  <><circle cx="24" cy="24" r="11.5" /><circle cx="24" cy="24" r="7.6" fill="var(--agent-avatar-bg)" /><circle cx="24" cy="24" r="3.7" /></>,
  <><ellipse cx="24" cy="24" rx="13" ry="5.4" fill="none" stroke="var(--agent-avatar-fg)" strokeWidth="3" transform="rotate(-24 24 24)" /><circle cx="24" cy="24" r="5.4" /></>,
  <path d="M24 12.5 35.6 33.5H12.4z" />,
  <><path d="M24 11 37 24 24 37 11 24z" /><path d="M24 17.5 30.5 24 24 30.5 17.5 24z" fill="var(--agent-avatar-bg)" /></>,
  <path d="M12.5 30 24 18.5 35.5 30" fill="none" stroke="var(--agent-avatar-fg)" strokeWidth="5" strokeLinecap="round" strokeLinejoin="round" />,
  <path d="M11 26q6.5-9.5 13 0t13 0" fill="none" stroke="var(--agent-avatar-fg)" strokeWidth="5" strokeLinecap="round" />,
  <><circle cx="24" cy="13.5" r="5" /><circle cx="24" cy="34.5" r="5" /><circle cx="13.5" cy="24" r="5" /><circle cx="34.5" cy="24" r="5" /></>,
];

export function isAgentAvatarKey(value: string): value is AgentAvatarKey {
  return AGENT_AVATAR_KEYS.some((key) => key === value);
}

export function randomAgentAvatarKey(): AgentAvatarKey {
  const value = new Uint32Array(1);
  window.crypto.getRandomValues(value);
  return AGENT_AVATAR_KEYS[value[0] % AGENT_AVATAR_KEYS.length];
}

export function AgentAvatarMark({ avatarKey, avatarImage }: { avatarKey: string; avatarImage?: string }): JSX.Element {
  if (avatarImage) return <img className="agent-avatar-mark agent-avatar-image" src={avatarImage} alt="" aria-hidden="true" />;
  const index = isAgentAvatarKey(avatarKey) ? AGENT_AVATAR_KEYS.indexOf(avatarKey) : 0;
  const style = {
    "--agent-avatar-bg": `var(--avatar-${index}-bg)`,
    "--agent-avatar-fg": `var(--avatar-${index}-fg)`,
  } as CSSProperties;
  return (
    <svg className="agent-avatar-mark" viewBox="0 0 48 48" style={style} aria-hidden="true">
      <rect width="48" height="48" fill="var(--agent-avatar-bg)" />
      <g fill="var(--agent-avatar-fg)">{MOTIFS[index]}</g>
    </svg>
  );
}
