import clsx from "clsx";

import { Globe } from "@/shared/assets/icons";

interface OrgWideNoticeProps {
  className?: string;
}

// Settings mixes org-wide subpages (preferences, security, team, …) with
// site-aware ones (schedules today). The topbar SitePicker lives in the
// global PageHeader and stays visible on every settings route, so an operator
// with a single site selected could reasonably assume it filters this page.
// This subtext makes the org-wide pages say so explicitly — the picker
// deliberately does not apply here. Rendered only when multi-site is enabled
// (no picker, no ambiguity otherwise); the caller owns that gate.
const OrgWideNotice = ({ className }: OrgWideNoticeProps) => (
  <div
    className={clsx("mb-6 flex items-center gap-2 text-200 text-text-primary-70", className)}
    data-testid="org-wide-notice"
  >
    <Globe className="shrink-0 text-text-primary-50" />
    <span>Org-wide · applies to all sites, not the selected site</span>
  </div>
);

export default OrgWideNotice;
