import type { ResponseProfile } from "@/protoFleet/features/settings/components/Curtailment/types";

export class AutomationResponseProfileRevisionConflictError extends Error {
  readonly latestResponseProfileRevisionById: ReadonlyMap<string, string>;

  constructor(message: string, responseProfiles: ResponseProfile[], cause: unknown) {
    super(message);
    this.name = "AutomationResponseProfileRevisionConflictError";
    this.latestResponseProfileRevisionById = new Map(
      responseProfiles.map((profile) => [profile.id, profile.revision ?? ""]),
    );
    (this as Error & { cause?: unknown }).cause = cause;
  }
}
