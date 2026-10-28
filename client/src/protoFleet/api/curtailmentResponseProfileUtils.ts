import { getCurtailmentScopeSummary, normalizeCurtailmentSelectionValues } from "@/protoFleet/api/curtailmentScopes";
import type { ResponseProfileFormValues } from "@/protoFleet/api/curtailmentTypes";
import { CurtailmentMode } from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { getSiteDisplayName } from "@/protoFleet/api/siteNames";

const responseProfileScopeLabelByMode: Partial<Record<CurtailmentMode, string>> = {
  [CurtailmentMode.FIXED_KW]: "Whole fleet",
  [CurtailmentMode.FULL_FLEET]: "Whole fleet",
};

function getResponseProfileScopeLabel(mode: CurtailmentMode): string {
  return responseProfileScopeLabelByMode[mode] ?? "Unknown scope";
}

export function getSelectedResponseProfileSiteIds(
  values: Pick<ResponseProfileFormValues, "siteSelection" | "siteId" | "siteIds">,
): string[] {
  const siteIds = normalizeCurtailmentSelectionValues(
    values.siteIds !== undefined && values.siteIds.length > 0 ? values.siteIds : values.siteId ? [values.siteId] : [],
  );

  return values.siteSelection === "site" ||
    values.siteSelection === "allSites" ||
    (values.siteSelection === undefined && siteIds.length > 0)
    ? siteIds
    : [];
}

export function getResponseProfileSiteNameForId(values: Partial<ResponseProfileFormValues>, siteId: string): string {
  return values.siteNamesById?.[siteId]?.trim() || (values.siteId === siteId ? values.siteName?.trim() : "") || "";
}

export function getResponseProfileSiteNamesById(
  values: ResponseProfileFormValues,
  siteIds: readonly string[],
): Record<string, string> {
  return Object.fromEntries(
    siteIds.map((siteId) => [siteId, getResponseProfileSiteNameForId(values, siteId) || getSiteDisplayName(siteId)]),
  );
}

export function getResponseProfileScopeSummary(
  values: ResponseProfileFormValues,
  mode: CurtailmentMode = values.actionType === "fixedKwReduction"
    ? CurtailmentMode.FIXED_KW
    : CurtailmentMode.FULL_FLEET,
): string {
  return getCurtailmentScopeSummary(values, {
    fallbackLabel: getResponseProfileScopeLabel(mode),
    getSiteLabel: (siteId) => getResponseProfileSiteNameForId(values, siteId),
  });
}
