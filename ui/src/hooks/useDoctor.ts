import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

/** Wire-level status values from `internal/doctor.Status*` constants. */
export type DoctorStatus = "ok" | "warn" | "err";

export interface DoctorCheck {
  name: string;
  status: DoctorStatus;
  message?: string;
  remediation?: string;
}

interface DoctorResponse {
  checks: DoctorCheck[];
}

/**
 * /api/doctor fetch. 30-second staleTime per spec — doctor results
 * don't change minute-to-minute, and a manual "Re-run checks" button
 * provides the escape hatch.
 */
export function useDoctor() {
  return useQuery({
    queryKey: ["doctor"],
    queryFn: async () => {
      const res = await api<DoctorResponse>("/api/doctor");
      return res.checks ?? [];
    },
    staleTime: 30_000,
  });
}
