import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from "react";
import { useNavigate } from "react-router";
import { Search } from "lucide-react";
import { Dialog as DialogPrimitive } from "radix-ui";
import { useSearch, type SearchMatch } from "@/hooks/useSearch";
import { relativeTime } from "@/lib/format";
import { cn } from "@/lib/utils";

interface SearchPaletteProps {
  open: boolean;
  onClose: () => void;
  /** Pre-filter results to a single session when opened from `/s/:name`. */
  sessionName?: string;
}

const DEBOUNCE_MS = 200;

/**
 * V19 Slice 2 — Linear/Raycast-style command palette for grep results.
 *
 * Composition:
 *  - radix Dialog for focus trap + Esc + outside-click close (built-in).
 *  - Debounced input (200ms) so each keystroke doesn't thunder against
 *    the search handler. We use a setTimeout cleanup, NOT lodash — one
 *    less dep behind the firewall.
 *  - Arrow up/down moves the highlighted row; Enter navigates.
 *
 * All styling uses the existing tokenised classes (bg, bg-surface,
 * border, fg, fg-muted, accent-gold) — no new palette introduced.
 */
export function SearchPalette({
  open,
  onClose,
  sessionName,
}: SearchPaletteProps) {
  const navigate = useNavigate();
  const [raw, setRaw] = useState("");
  const [debounced, setDebounced] = useState("");
  const [activeIdx, setActiveIdx] = useState(0);
  const listRef = useRef<HTMLUListElement | null>(null);
  const inputId = useId();

  // Reset state every time the palette opens so yesterday's query
  // doesn't greet tomorrow's user.
  useEffect(() => {
    if (!open) return;
    setRaw("");
    setDebounced("");
    setActiveIdx(0);
  }, [open]);

  // Debounce: wait 200ms after the last keystroke before committing the
  // query. Clearing on unmount/rerun ensures exactly one fetch per
  // settled keystroke.
  useEffect(() => {
    const id = window.setTimeout(() => setDebounced(raw), DEBOUNCE_MS);
    return () => window.clearTimeout(id);
  }, [raw]);

  const { data, isFetching } = useSearch(debounced, sessionName);
  const matches: SearchMatch[] = data?.matches ?? [];
  const truncated = Boolean(data?.truncated);
  const trimmed = debounced.trim();
  const hasQuery = trimmed.length >= 3;
  const showEmpty = hasQuery && !isFetching && matches.length === 0;

  // Clamp the active index whenever the list grows/shrinks.
  useEffect(() => {
    if (activeIdx >= matches.length) setActiveIdx(0);
  }, [matches.length, activeIdx]);

  const navigateToMatch = useCallback(
    (m: SearchMatch) => {
      // Approximate positioning: drop on the feed tab of the session.
      // Mapping hub-event-id to scroll anchor is out of scope for this
      // slice — SessionDetail defaults to newest-first, so recent hits
      // land above the fold.
      if (!m.session) return;
      navigate(`/s/${encodeURIComponent(m.session)}`);
      onClose();
    },
    [navigate, onClose],
  );

  function onInputKey(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActiveIdx((i) => Math.min(i + 1, Math.max(0, matches.length - 1)));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActiveIdx((i) => Math.max(0, i - 1));
    } else if (e.key === "Enter") {
      const row = matches[activeIdx];
      if (row) {
        e.preventDefault();
        navigateToMatch(row);
      }
    }
  }

  const listboxId = `${inputId}-listbox`;

  return (
    <DialogPrimitive.Root open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay
          className="fixed inset-0 z-50 bg-black/50 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:animate-in data-[state=open]:fade-in-0"
        />
        <DialogPrimitive.Content
          aria-label="Search"
          className={cn(
            "fixed left-[50%] top-[15%] z-50 w-full max-w-xl translate-x-[-50%]",
            "overflow-hidden rounded-lg border border-border bg-surface shadow-lg outline-none",
            "data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:animate-in data-[state=open]:fade-in-0",
          )}
        >
          <DialogPrimitive.Title className="sr-only">
            Search tool call history
          </DialogPrimitive.Title>
          <DialogPrimitive.Description className="sr-only">
            Type to search across tool call snippets. Use arrow keys to
            move between matches, Enter to open, Escape to close.
          </DialogPrimitive.Description>
          <div className="flex items-center gap-2 border-b border-border px-3 py-2.5">
            <Search
              size={14}
              className="shrink-0 text-fg-muted"
              aria-hidden
            />
            <input
              id={inputId}
              autoFocus
              type="text"
              role="combobox"
              aria-expanded={matches.length > 0}
              aria-controls={listboxId}
              aria-activedescendant={
                matches[activeIdx]
                  ? `${listboxId}-${activeIdx}`
                  : undefined
              }
              value={raw}
              onChange={(e) => {
                setRaw(e.target.value);
                setActiveIdx(0);
              }}
              onKeyDown={onInputKey}
              placeholder={
                sessionName
                  ? `Search in ${sessionName}…`
                  : "Search tool calls…"
              }
              className="min-w-0 flex-1 bg-transparent text-sm text-fg placeholder:text-fg-dim focus:outline-none"
            />
            {sessionName && (
              <span
                className="shrink-0 rounded border border-border bg-bg px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-[0.14em] text-fg-muted"
                aria-label={`Filtering to session ${sessionName}`}
              >
                {sessionName}
              </span>
            )}
          </div>

          <Results
            listboxId={listboxId}
            listRef={listRef}
            query={trimmed}
            matches={matches}
            activeIdx={activeIdx}
            onHover={setActiveIdx}
            onSelect={navigateToMatch}
            isFetching={isFetching}
            hasQuery={hasQuery}
            showEmpty={showEmpty}
            truncated={truncated}
          />
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

interface ResultsProps {
  listboxId: string;
  listRef: React.MutableRefObject<HTMLUListElement | null>;
  query: string;
  matches: SearchMatch[];
  activeIdx: number;
  onHover: (i: number) => void;
  onSelect: (m: SearchMatch) => void;
  isFetching: boolean;
  hasQuery: boolean;
  showEmpty: boolean;
  truncated: boolean;
}

function Results({
  listboxId,
  listRef,
  query,
  matches,
  activeIdx,
  onHover,
  onSelect,
  isFetching,
  hasQuery,
  showEmpty,
  truncated,
}: ResultsProps) {
  if (!hasQuery) {
    return (
      <div className="px-3 py-6 text-center text-xs text-fg-dim">
        Type at least 3 characters to search.
      </div>
    );
  }
  if (isFetching && matches.length === 0) {
    return (
      <div
        role="status"
        aria-live="polite"
        className="px-3 py-6 text-center text-xs text-fg-muted"
      >
        Searching…
      </div>
    );
  }
  if (showEmpty) {
    return (
      <div className="px-3 py-6 text-center text-xs text-fg-muted">
        No matches
      </div>
    );
  }
  return (
    <>
      <ul
        id={listboxId}
        ref={listRef}
        role="listbox"
        aria-label="Search results"
        className="max-h-[50vh] overflow-y-auto"
      >
        {matches.map((m, i) => (
          <ResultRow
            key={`${m.uuid}-${i}`}
            id={`${listboxId}-${i}`}
            match={m}
            query={query}
            active={i === activeIdx}
            onHover={() => onHover(i)}
            onSelect={() => onSelect(m)}
          />
        ))}
      </ul>
      {truncated && (
        <div
          role="status"
          className="border-t border-border bg-bg px-3 py-2 text-[11px] text-fg-muted"
        >
          Showing first {matches.length} of many — refine query.
        </div>
      )}
    </>
  );
}

interface ResultRowProps {
  id: string;
  match: SearchMatch;
  query: string;
  active: boolean;
  onHover: () => void;
  onSelect: () => void;
}

function ResultRow({
  id,
  match,
  query,
  active,
  onHover,
  onSelect,
}: ResultRowProps) {
  const highlighted = useMemo(
    () => highlightSnippet(match.snippet, query),
    [match.snippet, query],
  );
  const when = match.ts ? relativeTime(match.ts) : null;
  return (
    <li
      id={id}
      role="option"
      aria-selected={active}
      onMouseEnter={onHover}
    >
      <button
        type="button"
        onClick={onSelect}
        className={cn(
          "flex w-full items-baseline gap-3 px-3 py-2 text-left transition-colors",
          active ? "bg-surface-2" : "hover:bg-surface-2",
        )}
      >
        <span className="min-w-0 flex-1 truncate font-mono text-xs text-fg">
          {highlighted}
        </span>
        <span className="shrink-0 text-[10px] uppercase tracking-[0.14em] text-fg-muted">
          {match.session || "—"}
        </span>
        {match.tool && (
          <span className="shrink-0 text-[10px] text-fg-dim">{match.tool}</span>
        )}
        {when && (
          <time
            dateTime={match.ts}
            className="shrink-0 text-[10px] text-fg-dim tabular-nums"
          >
            {when} ago
          </time>
        )}
      </button>
    </li>
  );
}

/**
 * Case-insensitive highlighter. Splits the snippet around every literal
 * occurrence of `q` and wraps the match in a <mark> with the accent
 * colour. Keeps case of the original hit so "Bash" vs "bash" both read
 * naturally.
 */
function highlightSnippet(snippet: string, q: string): React.ReactNode {
  if (!q) return snippet;
  const lower = snippet.toLowerCase();
  const needle = q.toLowerCase();
  const parts: React.ReactNode[] = [];
  let i = 0;
  let key = 0;
  while (i < snippet.length) {
    const found = lower.indexOf(needle, i);
    if (found < 0) {
      parts.push(snippet.slice(i));
      break;
    }
    if (found > i) parts.push(snippet.slice(i, found));
    parts.push(
      <mark
        key={key++}
        className="bg-accent-gold/20 text-accent-gold"
        data-testid="search-hit"
      >
        {snippet.slice(found, found + needle.length)}
      </mark>,
    );
    i = found + needle.length;
  }
  return <>{parts}</>;
}
