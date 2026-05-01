// Shared input field for the LoginForm + SignupForm. Each form previously
// declared an identical local `Field` component — Sonar flagged the block
// as duplicated. Lifted here so both forms keep identical styling without
// drifting.

interface Props {
  label: string;
  value: string;
  onChange: (v: string) => void;
  type?: string;
  autoComplete?: string;
}

export function AuthField({ label, value, onChange, type = "text", autoComplete }: Props) {
  return (
    <label className="block">
      <span className="mb-1 block text-[11px] font-semibold uppercase tracking-[0.18em] text-fg-muted">
        {label}
      </span>
      <input
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        autoComplete={autoComplete}
        className="block w-full rounded border border-border bg-bg px-2 py-1.5 font-mono text-sm text-fg placeholder:text-fg-dim focus:outline-none focus:ring-1 focus:ring-accent-gold sm:text-xs"
      />
    </label>
  );
}
