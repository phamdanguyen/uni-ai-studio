// Shared utility functions used across frontend pages.

/** Map agent name to display icon. */
export function agentIcon(name: string): string {
  const icons: Record<string, string> = {
    director: "\u{1F3AC}",
    character: "\u{1F464}",
    location: "\u{1F3D4}",
    storyboard: "\u{1F39E}",
    media: "\u{1F5BC}",
    voice: "\u{1F3A4}",
  };
  return icons[name] || "\u{1F916}";
}

/** Format ISO timestamp to HH:MM:SS locale string. */
export function fmt(iso?: string): string {
  if (!iso) return "\u2014";
  return new Date(iso).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

/** Compute human-readable duration between two ISO timestamps. */
export function dur(start?: string, end?: string): string | null {
  if (!start) return null;
  const ms = new Date(end || new Date().toISOString()).getTime() - new Date(start).getTime();
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  return `${Math.floor(s / 60)}m ${s % 60}s`;
}

/** Safely parse a JSON string, returning null on failure. */
export function safeParseJson(str: string): unknown {
  try {
    return JSON.parse(str);
  } catch {
    return null;
  }
}
