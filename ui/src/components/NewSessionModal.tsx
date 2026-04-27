import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { Modal, Input, Textarea, Button } from "@ossrandom/design-system";
import { ApiError } from "@/lib/api";
import {
  isConflict,
  useCreateSession,
  type CreateConflict,
} from "@/hooks/useCreateSession";

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
  const [initialPrompt, setInitialPrompt] = useState<string>("");
  const [collision, setCollision] = useState<CreateConflict | null>(null);
  const [renaming, setRenaming] = useState(false);
  const [errMsg, setErrMsg] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setWorkdir(recents[0] ?? "");
    setName("");
    setInitialPrompt("");
    setCollision(null);
    setRenaming(false);
    setErrMsg(null);
    create.reset();
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  async function handleSubmit() {
    setErrMsg(null);
    try {
      const trimmedPrompt = initialPrompt.trim();
      const sess = await create.mutateAsync({
        workdir,
        ...(renaming && name ? { name } : {}),
        ...(trimmedPrompt ? { initial_prompt: trimmedPrompt } : {}),
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
    <Modal
      open={open}
      onClose={onClose}
      size="sm"
      title="New session"
      footer={
        collision ? (
          <CollisionFooter
            collision={collision}
            renaming={renaming}
            canSubmit={canSubmit}
            submitting={create.isPending}
            onClose={onClose}
            onGoExisting={() => {
              navigate(`/s/${encodeURIComponent(collision.session.name)}`);
              onClose();
            }}
            onRename={() => setRenaming(true)}
            onSubmit={handleSubmit}
          />
        ) : (
          <>
            <Button variant="secondary" size="sm" onClick={onClose}>
              Cancel
            </Button>
            <Button
              variant="primary"
              size="sm"
              type="submit"
              disabled={!canSubmit}
              loading={create.isPending}
              onClick={handleSubmit}
            >
              Create
            </Button>
          </>
        )
      }
    >
      {collision ? (
        <CollisionBody collision={collision} renaming={renaming} name={name} onName={setName} />
      ) : (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (canSubmit) handleSubmit();
          }}
        >
          <div className="rcs-label" style={{ marginBottom: 4 }}>Workdir</div>
          <Input
            value={workdir}
            onChange={(v) => setWorkdir(v)}
            placeholder="/home/dev/projects/…"
            autoFocus
            size="sm"
            aria-label="Workdir"
          />

          {recents.length > 0 && (
            <div style={{ marginTop: 12 }}>
              <div className="rcs-label" style={{ marginBottom: 4 }}>Recents</div>
              <ul role="list" style={{ display: "flex", flexDirection: "column", gap: 2, margin: 0, padding: 0, listStyle: "none" }}>
                {recents.map((r) => (
                  <li key={r}>
                    <Button
                      variant="ghost"
                      size="xs"
                      block
                      aria-label={r}
                      onClick={() => setWorkdir(r)}
                    >
                      <span style={{ fontFamily: "var(--font-mono)", fontSize: 12, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", textAlign: "left", width: "100%" }}>
                        {r}
                      </span>
                    </Button>
                  </li>
                ))}
              </ul>
            </div>
          )}

          <div className="rcs-label" style={{ marginTop: 16, marginBottom: 4 }}>
            Initial prompt
            <span style={{ marginLeft: 6, fontWeight: "normal", textTransform: "none", letterSpacing: 0, color: "var(--fg-3)" }}>
              (optional — sent after boot)
            </span>
          </div>
          <Textarea
            value={initialPrompt}
            onChange={(v) => setInitialPrompt(v)}
            placeholder="e.g. review the diff on main and suggest follow-ups"
            rows={3}
            size="sm"
            aria-label="Initial prompt"
          />

          {errMsg && (
            <div role="alert" style={{ marginTop: 10, fontSize: 12, color: "var(--danger)" }}>
              {errMsg}
            </div>
          )}
        </form>
      )}
    </Modal>
  );
}

function CollisionBody({
  collision,
  renaming,
  name,
  onName,
}: {
  collision: CreateConflict;
  renaming: boolean;
  name: string;
  onName: (v: string) => void;
}) {
  return (
    <div role="alert" style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <p style={{ margin: 0, fontSize: 14, color: "var(--fg-1)" }}>
        A session named{" "}
        <code style={{ background: "var(--bg-2)", padding: "1px 4px", borderRadius: 3, fontFamily: "var(--font-mono)", fontSize: 12 }}>
          {collision.session.name}
        </code>{" "}
        already exists for{" "}
        <code style={{ fontFamily: "var(--font-mono)", fontSize: 12, wordBreak: "break-all" }}>
          {collision.session.workdir}
        </code>
        .
      </p>

      {renaming && (
        <div>
          <div className="rcs-label" style={{ marginBottom: 4 }}>New name</div>
          <Input value={name} onChange={(v) => onName(v)} size="sm" aria-label="New name" />
        </div>
      )}
    </div>
  );
}

function CollisionFooter({
  collision: _collision,
  renaming,
  canSubmit,
  submitting,
  onClose: _onClose,
  onGoExisting,
  onRename,
  onSubmit,
}: {
  collision: CreateConflict;
  renaming: boolean;
  canSubmit: boolean;
  submitting: boolean;
  onClose: () => void;
  onGoExisting: () => void;
  onRename: () => void;
  onSubmit: () => void;
}) {
  return (
    <>
      <Button variant="secondary" size="sm" onClick={onGoExisting}>
        Go to existing
      </Button>
      {renaming ? (
        <Button
          variant="primary"
          size="sm"
          disabled={!canSubmit || submitting}
          loading={submitting}
          onClick={onSubmit}
        >
          Create
        </Button>
      ) : (
        <Button variant="secondary" size="sm" onClick={onRename}>
          Rename
        </Button>
      )}
    </>
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
