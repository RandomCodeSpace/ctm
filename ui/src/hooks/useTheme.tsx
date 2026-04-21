import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

export type ThemePreference = "system" | "light" | "dark";
export type ResolvedTheme = "light" | "dark";

const KEY = "ctm.theme";
const ORDER: ThemePreference[] = ["system", "light", "dark"];

interface ThemeCtx {
  preference: ThemePreference;
  resolved: ResolvedTheme;
  cycle: () => void;
  setPreference: (p: ThemePreference) => void;
}

const Ctx = createContext<ThemeCtx | null>(null);

function loadPref(): ThemePreference {
  try {
    const v = localStorage.getItem(KEY);
    if (v === "light" || v === "dark" || v === "system") return v;
  } catch {
    /* ignore */
  }
  return "system";
}

function resolve(p: ThemePreference): ResolvedTheme {
  if (p !== "system") return p;
  if (typeof window === "undefined") return "dark";
  return window.matchMedia("(prefers-color-scheme: light)").matches
    ? "light"
    : "dark";
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [preference, setPreferenceState] = useState<ThemePreference>(loadPref);
  const [resolved, setResolved] = useState<ResolvedTheme>(() =>
    resolve(loadPref()),
  );

  // Recompute resolved theme whenever preference or system colour-scheme changes.
  useEffect(() => {
    const update = () => setResolved(resolve(preference));
    update();
    if (preference !== "system") return;
    const mq = window.matchMedia("(prefers-color-scheme: light)");
    mq.addEventListener("change", update);
    return () => mq.removeEventListener("change", update);
  }, [preference]);

  // Apply to <html data-theme="…">.
  useEffect(() => {
    document.documentElement.dataset.theme = resolved;
  }, [resolved]);

  const setPreference = useCallback((p: ThemePreference) => {
    try {
      localStorage.setItem(KEY, p);
    } catch {
      /* ignore */
    }
    setPreferenceState(p);
  }, []);

  const cycle = useCallback(() => {
    setPreference(ORDER[(ORDER.indexOf(preference) + 1) % ORDER.length]);
  }, [preference, setPreference]);

  const value = useMemo<ThemeCtx>(
    () => ({ preference, resolved, cycle, setPreference }),
    [preference, resolved, cycle, setPreference],
  );

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useTheme(): ThemeCtx {
  const v = useContext(Ctx);
  if (!v) throw new Error("useTheme must be used inside <ThemeProvider>");
  return v;
}
