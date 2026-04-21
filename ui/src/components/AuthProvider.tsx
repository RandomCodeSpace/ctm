import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { QueryCache, useQueryClient } from "@tanstack/react-query";
import {
  TOKEN_KEY,
  UnauthorizedError,
  clearToken,
  getToken,
  setToken,
} from "@/lib/api";
import { TokenPasteScreen } from "@/components/TokenPasteScreen";

interface AuthCtx {
  token: string | null;
  setTokenAndPersist: (t: string) => void;
  signOut: () => void;
}

const Ctx = createContext<AuthCtx | null>(null);

/**
 * Holds the bearer token. If absent on first paint, renders <TokenPasteScreen>
 * and short-circuits the rest of the app. Mid-session 401s (from REST or SSE)
 * clear the token and navigate to /auth?next=<current> so the user returns
 * to where they were after re-pasting.
 */
export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const [token, setTokenState] = useState<string | null>(() => getToken());

  const setTokenAndPersist = useCallback((t: string) => {
    setToken(t);
    setTokenState(t);
  }, []);

  const signOut = useCallback(() => {
    clearToken();
    setTokenState(null);
    const next = window.location.pathname + window.location.search;
    if (window.location.pathname !== "/auth") {
      window.history.replaceState(
        {},
        "",
        `/auth?next=${encodeURIComponent(next)}`,
      );
    }
  }, []);

  // Listen to other tabs clearing the token.
  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === TOKEN_KEY) setTokenState(e.newValue);
    };
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  }, []);

  // Subscribe to TanStack Query failures — 401s from any query trigger sign-out.
  useEffect(() => {
    const cache: QueryCache = queryClient.getQueryCache();
    const unsub = cache.subscribe((event) => {
      if (event.type === "updated" && event.action.type === "error") {
        const err = event.action.error;
        if (err instanceof UnauthorizedError) signOut();
      }
    });
    return unsub;
  }, [queryClient, signOut]);

  const value = useMemo<AuthCtx>(
    () => ({ token, setTokenAndPersist, signOut }),
    [token, setTokenAndPersist, signOut],
  );

  if (!token) {
    return (
      <Ctx.Provider value={value}>
        <TokenPasteScreen />
      </Ctx.Provider>
    );
  }

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useAuth(): AuthCtx {
  const v = useContext(Ctx);
  if (!v) throw new Error("useAuth must be used inside <AuthProvider>");
  return v;
}
