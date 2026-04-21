/**
 * Tool → glyph + colour map. Glyphs are lucide icon names; the consumer
 * imports the icon by name. Colours are Tailwind utility class fragments
 * referencing tokens defined in src/index.css.
 */

import type { LucideIcon } from "lucide-react";
import {
  Terminal,
  FileEdit,
  FileText,
  FilePlus,
  Search,
  Globe,
  ListTree,
  Sparkles,
  Wrench,
} from "lucide-react";

export type ToolName =
  | "Bash"
  | "Edit"
  | "Read"
  | "Write"
  | "Glob"
  | "Grep"
  | "WebFetch"
  | "Task"
  | string;

export interface ToolDescriptor {
  icon: LucideIcon;
  /** Tailwind class fragment, e.g. "text-fg-muted". */
  colorClass: string;
}

const MAP: Record<string, ToolDescriptor> = {
  Bash: { icon: Terminal, colorClass: "text-fg" },
  Edit: { icon: FileEdit, colorClass: "text-accent-gold" },
  Read: { icon: FileText, colorClass: "text-fg-muted" },
  Write: { icon: FilePlus, colorClass: "text-accent-gold" },
  Glob: { icon: ListTree, colorClass: "text-fg-muted" },
  Grep: { icon: Search, colorClass: "text-fg-muted" },
  WebFetch: { icon: Globe, colorClass: "text-fg-muted" },
  Task: { icon: Sparkles, colorClass: "text-accent-gold" },
};

const FALLBACK: ToolDescriptor = {
  icon: Wrench,
  colorClass: "text-fg-dim",
};

export function toolDescriptor(name: ToolName): ToolDescriptor {
  return MAP[name] ?? FALLBACK;
}
