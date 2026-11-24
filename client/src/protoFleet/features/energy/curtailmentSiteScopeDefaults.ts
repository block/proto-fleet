import { getValidSiteScopeId } from "@/protoFleet/api/curtailmentScopes";
import type { CurtailmentSiteOption } from "@/protoFleet/api/curtailmentTypes";

export function getDefaultCurtailmentSiteScope(
  activeSite: { kind: string; id?: string },
  siteOptions: readonly CurtailmentSiteOption[],
): CurtailmentSiteOption | undefined {
  if (activeSite.kind !== "site") {
    return undefined;
  }

  const siteId = getValidSiteScopeId(activeSite.id);
  if (!siteId) {
    return undefined;
  }

  const selectedSiteOption = siteOptions.find((siteOption) => siteOption.id === siteId);
  if (selectedSiteOption) {
    return selectedSiteOption;
  }

  return siteOptions.length === 0 ? { id: siteId, name: `Site ${siteId}` } : undefined;
}
