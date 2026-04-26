import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Navigate, RouterProvider, createBrowserRouter } from "react-router";
import { useMemo } from "react";
import { ThemeProvider } from "@/hooks/useTheme";
import { AuthProvider } from "@/components/AuthProvider";
import { SseProvider } from "@/components/SseProvider";
import { Dashboard } from "@/routes/Dashboard";
import { DoctorPanel } from "@/routes/DoctorPanel";
import { FeedFullscreen } from "@/routes/FeedFullscreen";
import { ConnectionBanner } from "@/components/ConnectionBanner";
import { AuthGate } from "@/routes/AuthGate";

/*
 * Routing intent: in two-pane mode (>=768px) the Dashboard owns both
 * panels, so /, /s/:name, /s/:name/checkpoints, /s/:name/meta all
 * resolve to <Dashboard>. The right pane reads useParams() to swap
 * between the empty placeholder and <SessionDetail>. The list never
 * unmounts. On mobile (<768px), Dashboard hides the right pane entirely
 * and the list takes over — when a session is selected, Dashboard hides
 * the list and shows the detail (responsive layout, single Dashboard
 * route stays mounted).
 *
 * This keeps URL semantics (deep-link, browser back) consistent across
 * widths, and matches spec §3 (Desktop scaling: Two-pane).
 */
const router = createBrowserRouter([
  { path: "/", element: <Dashboard /> },
  { path: "/s/:name", element: <Dashboard /> },
  { path: "/s/:name/feed", element: <Dashboard /> },
  { path: "/s/:name/checkpoints", element: <Dashboard /> },
  { path: "/s/:name/pane", element: <Dashboard /> },
  { path: "/s/:name/subagents", element: <Dashboard /> },
  { path: "/s/:name/teams", element: <Dashboard /> },
  { path: "/s/:name/meta", element: <Dashboard /> },
  { path: "/feed", element: <FeedFullscreen /> },
  { path: "/doctor", element: <DoctorPanel /> },
  { path: "*", element: <Navigate to="/" replace /> },
]);

export function App() {
  const queryClient = useMemo(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            staleTime: 30_000,
            refetchOnWindowFocus: false,
            retry: (failureCount, err) => {
              // Bail on auth errors — AuthProvider handles redirect.
              if (err instanceof Error && err.name === "UnauthorizedError") {
                return false;
              }
              return failureCount < 2;
            },
          },
        },
      }),
    [],
  );

  return (
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>
        <AuthProvider>
          <SseProvider>
            <AuthGate>
              <ConnectionBanner />
              <RouterProvider router={router} />
            </AuthGate>
          </SseProvider>
        </AuthProvider>
      </QueryClientProvider>
    </ThemeProvider>
  );
}
