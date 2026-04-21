import { Monitor, Moon, Sun } from "lucide-react";
import { useTheme, type ThemePreference } from "@/hooks/useTheme";

const ICON: Record<ThemePreference, typeof Sun> = {
  system: Monitor,
  light: Sun,
  dark: Moon,
};

const LABEL: Record<ThemePreference, string> = {
  system: "Theme: system",
  light: "Theme: light",
  dark: "Theme: dark",
};

/**
 * Header sun/moon button cycling System → Light → Dark.
 * Persisted to localStorage["ctm.theme"] by useTheme.
 */
export function ThemeToggle() {
  const { preference, cycle } = useTheme();
  const Icon = ICON[preference];
  return (
    <button
      type="button"
      onClick={cycle}
      aria-label={LABEL[preference]}
      title={LABEL[preference]}
      className="inline-flex h-8 w-8 items-center justify-center rounded text-fg-muted transition-colors hover:bg-surface-2 hover:text-fg"
    >
      <Icon size={16} aria-hidden />
    </button>
  );
}
