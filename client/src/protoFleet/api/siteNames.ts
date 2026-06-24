import type { SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";

export type SiteNameById = ReadonlyMap<string, string>;

export function buildSiteNameById(sites: readonly SiteWithCounts[] | undefined): Map<string, string> {
  const siteNameById = new Map<string, string>();

  for (const { site } of sites ?? []) {
    const siteId = site?.id?.toString() ?? "0";
    const siteName = site?.name?.trim() ?? "";

    if (siteId !== "0" && siteName) {
      siteNameById.set(siteId, siteName);
    }
  }

  return siteNameById;
}

export function getSiteDisplayName(siteId: bigint | string, siteNameById?: SiteNameById): string {
  const siteIdText = siteId.toString().trim();
  if (!siteIdText || siteIdText === "0") {
    return "";
  }

  return siteNameById?.get(siteIdText)?.trim() || `Site ${siteIdText}`;
}
