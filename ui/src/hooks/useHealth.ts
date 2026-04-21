import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

export interface HealthComponent {
  name: string;
  status: "ok" | "degraded" | "down";
  detail?: string;
}

export interface Health {
  status: "ok" | "degraded";
  components: HealthComponent[];
}

export function useHealth() {
  return useQuery<Health>({
    queryKey: ["health"],
    queryFn: () => api<Health>("/health"),
    refetchInterval: 30_000,
  });
}
