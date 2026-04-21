import { Skeleton } from "@/components/ui/skeleton";
import { useLogsUsage } from "@/hooks/useLogsUsage";
import { humanBytes, relativeTime } from "@/lib/format";

/**
 * V21 log disk usage card. Mounted below the Meta tab fields on
 * SessionDetail so users notice when it's time to prune
 * ~/.config/ctm/logs.
 *
 * Read-only by design — no delete button. If users want to prune they
 * do it with `rm` / a housekeeping script; exposing deletion from the
 * UI would be a second-axis auth decision we don't want to make in v1.
 */
export function LogDiskUsage() {
  const { data, isLoading, isError, error } = useLogsUsage();

  return (
    <section
      aria-label="Log disk usage"
      className="mx-6 my-4 border border-border bg-surface"
    >
      <header className="flex items-baseline justify-between border-b border-border px-4 py-2">
        <h3 className="text-[11px] font-semibold uppercase tracking-[0.18em] text-fg-muted">
          Log disk usage
        </h3>
        {data && (
          <span
            className="font-mono text-sm tabular-nums text-fg"
            aria-label={`Total ${humanBytes(data.total_bytes)}`}
          >
            {humanBytes(data.total_bytes)}
          </span>
        )}
      </header>

      {isLoading && (
        <div className="space-y-2 p-4">
          <Skeleton className="h-4 w-1/2" />
          <Skeleton className="h-4 w-3/4" />
          <Skeleton className="h-4 w-2/3" />
        </div>
      )}

      {isError && (
        <p
          role="alert"
          className="m-4 border-l-[3px] border-alert-ember bg-bg px-3 py-2 text-sm text-alert-ember"
        >
          Could not load log usage
          {error instanceof Error ? `: ${error.message}` : ""}
        </p>
      )}

      {data && !isLoading && !isError && (
        <>
          {data.files.length === 0 ? (
            <p className="px-4 py-6 text-center text-sm text-fg-dim">
              No log files in {data.dir}
            </p>
          ) : (
            <table className="w-full text-sm">
              <caption className="sr-only">
                Per-session log file sizes, sorted by bytes descending
              </caption>
              <thead>
                <tr className="text-left text-[11px] font-semibold uppercase tracking-[0.18em] text-fg-muted">
                  <th scope="col" className="px-4 py-2 font-semibold">
                    Session
                  </th>
                  <th
                    scope="col"
                    className="px-4 py-2 text-right font-semibold"
                  >
                    Size
                  </th>
                  <th scope="col" className="px-4 py-2 font-semibold">
                    Updated
                  </th>
                </tr>
              </thead>
              <tbody>
                {data.files.map((f) => (
                  <tr
                    key={f.uuid}
                    className="border-t border-border hover:bg-bg"
                  >
                    <td className="px-4 py-2">
                      <code
                        className="font-mono text-fg"
                        title={f.uuid}
                      >
                        {f.session}
                      </code>
                    </td>
                    <td className="px-4 py-2 text-right font-mono tabular-nums text-fg">
                      {humanBytes(f.bytes)}
                    </td>
                    <td className="px-4 py-2 text-fg-dim">
                      {f.mtime ? (
                        <time dateTime={f.mtime}>
                          {relativeTime(f.mtime)} ago
                        </time>
                      ) : (
                        "—"
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          <footer className="border-t border-border px-4 py-2 text-[11px] text-fg-dim">
            <code className="font-mono" title={data.dir}>
              {data.dir}
            </code>
          </footer>
        </>
      )}
    </section>
  );
}
