import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { Dialog as DialogPrimitive } from "radix-ui";
import { X } from "lucide-react";
import { ApiError } from "@/lib/api";
import {
  isConflict,
  useCreateSession,
  type CreateConflict,
} from "@/hooks/useCreateSession";
import { cn } from "@/lib/utils";

interface NewSessionModalProps {
  open: boolean;
  onClose: () => void;
  recents: string[];
}

/**
 * V26 — create a fresh yolo-mode claude session from the browser.
 * Flow:
 *   default state: workdir input (pre-filled with recents[0]) + Create
 *   on 201: navigate("/s/<name>") + onClose()
 *   on 409: swap to collision state with Rename / Go-to-existing
 *   on 4xx/5xx: inline error message
 */
export function NewSessionModal({ open, onClose, recents }: NewSessionModalProps) {
  const navigate = useNavigate();
  const create = useCreateSession();
  const [workdir, setWorkdir] = useState(recents[0] ?? "");
  const [name, setName] = useState<string>("");
  const [collision, setCollision] = useState<CreateConflict | null>(null);
  const [renaming, setRenaming] = useState(false);
  const [errMsg, setErrMsg] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setWorkdir(recents[0] ?? "");
    setName("");
    setCollision(null);
    setRenaming(false);
    setErrMsg(null);
    create.reset();
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  async function handleSubmit() {
    setErrMsg(null);
    try {
      const sess = await create.mutateAsync({
        workdir,
        ...(renaming && name ? { name } : {}),
      });
      navigate(`/s/${encodeURIComponent(sess.name)}`);
      onClose();
    } catch (e) {
      if (isConflict(e)) {
        setCollision(e.body);
        setName(suggestRename(e.body.session.name));
        return;
      }
      setErrMsg(serverMessage(e) ?? "Could not create session");
    }
  }

  const canSubmit = workdir.trim().length > 0 && !create.isPending;

  return (
    <DialogPrimitive.Root open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/50" />
        <DialogPrimitive.Content
          className={cn(
            "fixed left-[50%] top-[15%] z-50 w-full max-w-md translate-x-[-50%]",
            "overflow-hidden rounded-lg border border-border bg-surface shadow-lg outline-none",
            "p-4",
          )}
        >
          <div className="mb-3 flex items-center justify-between">
            <DialogPrimitive.Title className="font-serif text-lg font-bold">
              New session
            </DialogPrimitive.Title>
            <button
              type="button"
              aria-label="Close"
              onClick={onClose}
              className="rounded p-1 text-fg-muted hover:bg-surface-2 hover:text-fg"
            >
              <X size={14} aria-hidden />
            </button>
          </div>

          {collision ? (
            <CollisionPanel
              collision={collision}
              name={name}
              onName={setName}
              onGoExisting={() => {
                navigate(
                  `/s/${encodeURIComponent(collision.session.name)}`,
                );
                onClose();
              }}
              onRename={() => {
                setRenaming(true);
              }}
              renaming={renaming}
              onSubmit={handleSubmit}
              submitting={create.isPending}
              canSubmit={canSubmit}
            />
          ) : (
            <form
              onSubmit={(e) => {
                e.preventDefault();
                if (canSubmit) handleSubmit();
              }}
            >
              <label className="mb-1 block text-[11px] font-semibold uppercase tracking-[0.18em] text-fg-muted">
                Workdir
              </label>
              <input
                type="text"
                aria-label="Workdir"
                value={workdir}
                onChange={(e) => setWorkdir(e.target.value)}
                placeholder="/home/dev/projects/…"
                autoFocus
                className="mb-2 block w-full rounded border border-border bg-bg px-2 py-1.5 font-mono text-xs text-fg placeholder:text-fg-dim focus:outline-none focus:ring-1 focus:ring-accent-gold"
              />

              {recents.length > 0 && (
                <div className="mb-3">
                  <div className="mb-1 text-[10px] uppercase tracking-[0.14em] text-fg-dim">
                    Recents
                  </div>
                  <ul className="space-y-1">
                    {recents.map((r) => (
                      <li key={r}>
                        <button
                          type="button"
                          aria-label={r}
                          onClick={() => setWorkdir(r)}
                          className="block w-full truncate rounded px-2 py-1 text-left font-mono text-xs text-fg hover:bg-surface-2"
                        >
                          {r}
                        </button>
                      </li>
                    ))}
                  </ul>
                </div>
              )}

              {errMsg && (
                <div
                  role="alert"
                  className="mb-2 text-[11px] text-alert-ember"
                >
                  {errMsg}
                </div>
              )}

              <div className="flex justify-end gap-2">
                <button
                  type="button"
                  onClick={onClose}
                  className="rounded border border-border bg-surface px-3 py-1 text-xs text-fg hover:bg-surface-2"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={!canSubmit}
                  className="rounded border border-border bg-accent-gold px-3 py-1 text-xs font-semibold text-bg hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  Create
                </button>
              </div>
            </form>
          )}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

function CollisionPanel({
  collision,
  name,
  onName,
  onGoExisting,
  onRename,
  renaming,
  onSubmit,
  submitting,
  canSubmit,
}: {
  collision: CreateConflict;
  name: string;
  onName: (v: string) => void;
  onGoExisting: () => void;
  onRename: () => void;
  renaming: boolean;
  onSubmit: () => void;
  submitting: boolean;
  canSubmit: boolean;
}) {
  return (
    <div role="alert" className="space-y-3">
      <p className="text-sm text-fg">
        A session named{" "}
        <code className="rounded bg-bg px-1 py-0.5 font-mono text-xs">
          {collision.session.name}
        </code>{" "}
        already exists for{" "}
        <code className="break-all font-mono text-xs">
          {collision.session.workdir}
        </code>
        .
      </p>

      {renaming ? (
        <div>
          <label className="mb-1 block text-[11px] font-semibold uppercase tracking-[0.18em] text-fg-muted">
            New name
          </label>
          <input
            type="text"
            aria-label="New name"
            value={name}
            onChange={(e) => onName(e.target.value)}
            className="block w-full rounded border border-border bg-bg px-2 py-1.5 font-mono text-xs text-fg focus:outline-none focus:ring-1 focus:ring-accent-gold"
          />
        </div>
      ) : null}

      <div className="flex justify-end gap-2">
        <button
          type="button"
          onClick={onGoExisting}
          className="rounded border border-border bg-surface px-3 py-1 text-xs text-fg hover:bg-surface-2"
        >
          Go to existing
        </button>
        {renaming ? (
          <button
            type="button"
            onClick={onSubmit}
            disabled={!canSubmit || submitting}
            className="rounded border border-border bg-accent-gold px-3 py-1 text-xs font-semibold text-bg hover:brightness-110 disabled:opacity-40"
          >
            Create
          </button>
        ) : (
          <button
            type="button"
            onClick={onRename}
            className="rounded border border-border bg-surface px-3 py-1 text-xs text-fg hover:bg-surface-2"
          >
            Rename
          </button>
        )}
      </div>
    </div>
  );
}

function suggestRename(name: string): string {
  const m = /^(.*?)-(\d+)$/.exec(name);
  if (m) return `${m[1]}-${parseInt(m[2], 10) + 1}`;
  return `${name}-2`;
}

function serverMessage(e: unknown): string | undefined {
  if (e instanceof ApiError && typeof e.body === "object" && e.body !== null) {
    const m = (e.body as { message?: unknown }).message;
    if (typeof m === "string") return m;
  }
  return undefined;
}
