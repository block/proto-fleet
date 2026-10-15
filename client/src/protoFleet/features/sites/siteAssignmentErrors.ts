import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { isPermissionDeniedError } from "@/protoFleet/api/requestErrors";

export function getSiteAssignmentErrorMessage(error: unknown): string {
  if (isPermissionDeniedError(error)) {
    return "You don't have permission to change site assignments. Ask an administrator for access to manage the affected sites.";
  }
  return getErrorMessage(error, "The site assignment couldn't be saved. Try again.");
}
