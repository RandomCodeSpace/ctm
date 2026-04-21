import { useEffect, useMemo, useState } from "react";
import { Loader2 } from "lucide-react";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import {
  useConfigGet,
  useConfigUpdate,
  type AttentionThresholds,
  type ConfigPayload,
} from "@/hooks/useConfigUpdate";

/**
 * V0.2 Settings drawer. Edits the same allowlisted subset of
 * ~/.config/ctm/config.json that PATCH /api/config accepts:
 *
 *   - webhook_url / webhook_auth
 *   - six attention.* thresholds
 *
 * Save-then-restart flow: the daemon writes config atomically, returns
 * 202, then cancels its root context ~1s later. Users stay on this
 * page; ConnectionBanner surfaces the disconnect and SSE reconnects
 * once `ctm attach/new/yolo` respawns the daemon.
 */
interface SettingsDrawerProps {
  open: boolean;
  onClose: () => void;
}

type SaveState =
  | { kind: "idle" }
  | { kind: "saving" }
  | { kind: "restarting" }
  | { kind: "error"; message: string };

interface FormState {
  webhook_url: string;
  webhook_auth: string;
  attention: AttentionThresholds;
}

const THRESHOLD_META: Array<{
  key: keyof AttentionThresholds;
  label: string;
  helper: string;
  unit: "pct" | "min" | "events";
  max: number;
}> = [
  {
    key: "error_rate_pct",
    label: "Error rate %",
    helper: "Alert when N% of recent tool calls fail.",
    unit: "pct",
    max: 100,
  },
  {
    key: "error_rate_window",
    label: "Error rate window",
    helper: "Number of recent tool calls evaluated for the error rate.",
    unit: "events",
    max: 1440,
  },
  {
    key: "idle_minutes",
    label: "Idle minutes",
    helper: "Alert when no tool calls happen for this many minutes.",
    unit: "min",
    max: 1440,
  },
  {
    key: "quota_pct",
    label: "Quota %",
    helper: "Alert when weekly/5-hour quota crosses this threshold.",
    unit: "pct",
    max: 100,
  },
  {
    key: "context_pct",
    label: "Context %",
    helper: "Alert when a session's context window is this full.",
    unit: "pct",
    max: 100,
  },
  {
    key: "yolo_unchecked_minutes",
    label: "Yolo unchecked minutes",
    helper: "Alert when a yolo session runs this long without a human check-in.",
    unit: "min",
    max: 1440,
  },
];

export function SettingsDrawer({ open, onClose }: SettingsDrawerProps) {
  const { data, isLoading } = useConfigGet(open);
  const mutation = useConfigUpdate();

  const [form, setForm] = useState<FormState | null>(null);
  const [save, setSave] = useState<SaveState>({ kind: "idle" });

  // Re-seed the form every time the drawer opens so stale edits from a
  // previous cancelled session don't bleed into a new one.
  useEffect(() => {
    if (open && data) {
      setForm({
        webhook_url: data.webhook_url ?? "",
        webhook_auth: data.webhook_auth ?? "",
        attention: { ...data.attention },
      });
      setSave({ kind: "idle" });
    }
  }, [open, data]);

  // Client-side validation mirrors the Go handler's range checks. The
  // button disables instead of shouting so the user can correct the
  // field without a round trip. Server re-validates — this is
  // convenience, not trust boundary.
  const validationError = useMemo(() => {
    if (!form) return null;
    for (const meta of THRESHOLD_META) {
      const v = form.attention[meta.key];
      if (!Number.isFinite(v) || v <= 0) {
        return `${meta.label} must be > 0`;
      }
      if (v > meta.max) {
        return `${meta.label} must be <= ${meta.max}`;
      }
    }
    return null;
  }, [form]);

  async function handleSave() {
    if (!form || validationError) return;
    setSave({ kind: "saving" });
    try {
      const body: ConfigPayload = {
        webhook_url: form.webhook_url,
        webhook_auth: form.webhook_auth,
        attention: form.attention,
      };
      await mutation.mutateAsync(body);
      setSave({ kind: "restarting" });
    } catch (err) {
      setSave({
        kind: "error",
        message: err instanceof Error ? err.message : "Save failed",
      });
    }
  }

  const disabled =
    !form ||
    save.kind === "saving" ||
    save.kind === "restarting" ||
    validationError !== null;

  return (
    <Sheet open={open} onOpenChange={(v) => !v && onClose()}>
      <SheetContent
        side="right"
        className="bg-surface text-fg w-full sm:max-w-md border-l border-border overflow-y-auto"
      >
        <SheetHeader className="border-b border-border">
          <SheetTitle className="font-serif text-xl text-fg">Settings</SheetTitle>
          <SheetDescription className="text-fg-muted">
            Saving restarts the daemon. Open sessions keep running.
          </SheetDescription>
        </SheetHeader>

        <div className="flex-1 space-y-6 px-4 py-6">
          {isLoading || !form ? (
            <p className="text-sm text-fg-muted" role="status">
              Loading current settings…
            </p>
          ) : (
            <>
              <WebhookFields
                form={form}
                onChange={(patch) =>
                  setForm((prev) => (prev ? { ...prev, ...patch } : prev))
                }
              />
              <ThresholdFields
                form={form}
                onChange={(key, value) =>
                  setForm((prev) =>
                    prev
                      ? {
                          ...prev,
                          attention: { ...prev.attention, [key]: value },
                        }
                      : prev,
                  )
                }
              />
              {validationError && (
                <p
                  role="alert"
                  className="border-l-[3px] border-alert-ember pl-3 text-sm text-alert-ember"
                >
                  {validationError}
                </p>
              )}
              {save.kind === "restarting" && (
                <div
                  role="status"
                  aria-live="polite"
                  className="rounded border border-border bg-surface-2 p-3"
                >
                  <p className="text-xs font-semibold uppercase tracking-[0.18em] text-live-dot">
                    Daemon restarting…
                  </p>
                  <p className="mt-1 text-xs text-fg-dim">
                    Keep this tab open. The banner will clear once ctm reconnects.
                  </p>
                </div>
              )}
              {save.kind === "error" && (
                <p
                  role="alert"
                  className="border-l-[3px] border-alert-ember pl-3 text-sm text-alert-ember"
                >
                  {save.message}
                </p>
              )}
            </>
          )}
        </div>

        <SheetFooter className="border-t border-border">
          <Button
            type="button"
            variant="outline"
            onClick={onClose}
            className="border-border bg-transparent text-fg hover:bg-surface-2"
          >
            Close
          </Button>
          <Button
            type="button"
            onClick={handleSave}
            disabled={disabled}
            className="bg-accent-gold text-bg hover:opacity-90 disabled:opacity-50"
          >
            {save.kind === "saving" || save.kind === "restarting" ? (
              <>
                <Loader2 size={14} className="animate-spin" aria-hidden />
                {save.kind === "saving" ? "Saving…" : "Restarting…"}
              </>
            ) : (
              "Save & restart daemon"
            )}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

interface WebhookFieldsProps {
  form: FormState;
  onChange: (patch: Partial<FormState>) => void;
}

function WebhookFields({ form, onChange }: WebhookFieldsProps) {
  return (
    <fieldset className="space-y-3">
      <legend className="text-[11px] font-semibold uppercase tracking-[0.18em] text-fg-muted">
        Webhook
      </legend>
      <label className="block space-y-1">
        <span className="text-xs text-fg-muted">Webhook URL</span>
        <input
          type="url"
          value={form.webhook_url}
          onChange={(e) => onChange({ webhook_url: e.target.value })}
          placeholder="https://hooks.example/ctm"
          className="w-full rounded border border-border bg-bg px-2 py-1.5 font-mono text-xs text-fg placeholder:text-fg-dim focus:border-accent-gold focus:outline-none"
          aria-label="Webhook URL"
        />
        <span className="block text-[11px] text-fg-dim">
          Empty disables outbound events.
        </span>
      </label>
      <label className="block space-y-1">
        <span className="text-xs text-fg-muted">Webhook Authorization</span>
        <input
          type="text"
          value={form.webhook_auth}
          onChange={(e) => onChange({ webhook_auth: e.target.value })}
          placeholder="Bearer …"
          className="w-full rounded border border-border bg-bg px-2 py-1.5 font-mono text-xs text-fg placeholder:text-fg-dim focus:border-accent-gold focus:outline-none"
          aria-label="Webhook Authorization"
        />
        <span className="block text-[11px] text-fg-dim">
          Sent verbatim as the Authorization header.
        </span>
      </label>
    </fieldset>
  );
}

interface ThresholdFieldsProps {
  form: FormState;
  onChange: (key: keyof AttentionThresholds, value: number) => void;
}

function ThresholdFields({ form, onChange }: ThresholdFieldsProps) {
  return (
    <fieldset className="space-y-3">
      <legend className="text-[11px] font-semibold uppercase tracking-[0.18em] text-fg-muted">
        Attention thresholds
      </legend>
      {THRESHOLD_META.map((meta) => {
        const unitLabel =
          meta.unit === "pct" ? "%" : meta.unit === "min" ? "min" : "events";
        return (
          <label key={meta.key} className="block space-y-1">
            <span className="flex items-center justify-between text-xs text-fg-muted">
              <span>{meta.label}</span>
              <span className="text-[11px] text-fg-dim">{unitLabel}</span>
            </span>
            <input
              type="number"
              min={1}
              max={meta.max}
              step={1}
              value={form.attention[meta.key]}
              onChange={(e) => onChange(meta.key, Number(e.target.value))}
              className="w-full rounded border border-border bg-bg px-2 py-1.5 font-mono text-xs text-fg focus:border-accent-gold focus:outline-none"
              aria-label={meta.label}
            />
            <span className="block text-[11px] text-fg-dim">{meta.helper}</span>
          </label>
        );
      })}
    </fieldset>
  );
}
