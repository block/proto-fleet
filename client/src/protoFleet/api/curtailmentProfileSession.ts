import type { ResponseProfileFormValues } from "./curtailmentTypes";
export const sessionFormValuesByProfileId = new Map<string, ResponseProfileFormValues>();
export function resetCurtailmentProfileSession(): void {
  sessionFormValuesByProfileId.clear();
}
