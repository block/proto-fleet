import { type CSSProperties, useState } from "react";
import { Link } from "react-router-dom";
import SiteCard from "./SiteCard";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import SectionHeading from "@/protoFleet/features/dashboard/components/SectionHeading";
import { ChevronDown } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

// Track gap between cards (Tailwind gap-1 = 4px). Drives both the card width
// and the per-step translate so they can't drift.
const GAP_PX = 4;

interface SitesSectionProps {
  // `undefined` while ListSites is still loading; `[]` once loaded with no
  // sites in the org.
  sites: SiteWithCounts[] | undefined;
}

// All-Sites "Sites" section: a horizontal gallery of site cards that overflows
// the content width. How many cards fit at once is responsive (desktop 3,
// laptop/tablet 2, phone 1); the chevrons slide the track one card at a time,
// clamped so the last card right-aligns with the page content.
const SitesSection = ({ sites }: SitesSectionProps) => {
  const { isDesktop, isPhone } = useWindowDimensions();
  const visible = isDesktop ? 3 : isPhone ? 1 : 2;

  const [index, setIndex] = useState(0);

  const maxIndex = sites ? Math.max(0, sites.length - visible) : 0;
  // Clamp the offset when the visible count grows (resize to a wider
  // breakpoint) or the site list shrinks under us.
  const safeIndex = Math.min(index, maxIndex);

  const showPagination = (sites?.length ?? 0) > visible;

  // One card width + the slide step, both derived from `visible`. The `100%`
  // resolves against the track (content width), so `visible` cards exactly
  // fill it. The step is card + gap; the index clamp above keeps the last
  // card flush with the content edge.
  const cardWidth = `calc((100% - ${(visible - 1) * GAP_PX}px) / ${visible})`;
  const trackStyle: CSSProperties & { "--site-card-w": string } = {
    "--site-card-w": cardWidth,
    transform: `translateX(calc(-${safeIndex} * (var(--site-card-w) + ${GAP_PX}px)))`,
  };

  return (
    <section className="pb-6" data-testid="dashboard-sites-section">
      <div className="px-6 laptop:px-10">
        <SectionHeading heading="Sites">
          <div className="flex items-center gap-2">
            {showPagination ? (
              <div className="flex items-center gap-1">
                <Button
                  variant={variants.secondary}
                  size={sizes.compact}
                  ariaLabel="Previous sites"
                  disabled={safeIndex === 0}
                  onClick={() => setIndex(Math.max(0, safeIndex - 1))}
                  prefixIcon={<ChevronDown className="rotate-90" />}
                  testId="dashboard-sites-prev"
                />
                <Button
                  variant={variants.secondary}
                  size={sizes.compact}
                  ariaLabel="Next sites"
                  disabled={safeIndex >= maxIndex}
                  onClick={() => setIndex(Math.min(maxIndex, safeIndex + 1))}
                  prefixIcon={<ChevronDown className="-rotate-90" />}
                  testId="dashboard-sites-next"
                />
              </div>
            ) : null}
            <Link to="/fleet/sites" data-testid="dashboard-sites-view-all">
              <Button variant={variants.secondary} size={sizes.compact} text="View sites" />
            </Link>
          </div>
        </SectionHeading>
      </div>

      {/* The clip spans the full content area to the screen edge; the track
          stays at content width (mx matches the page padding) so the cards
          overflow into the right gutter instead of being cut at the content
          boundary, while the clamp still right-aligns the last card. */}
      <div className="mt-6 overflow-hidden">
        <div
          className="mx-6 flex gap-1 transition-transform duration-300 ease-out laptop:mx-10"
          style={trackStyle}
          data-testid="dashboard-sites-track"
        >
          {sites === undefined
            ? Array.from({ length: visible }).map((_, i) => (
                <SkeletonBar key={i} className="h-44 w-[var(--site-card-w)] shrink-0 rounded-xl" />
              ))
            : sites.map((site) => (
                <SiteCard
                  key={(site.site?.id ?? 0n).toString()}
                  site={site}
                  className="w-[var(--site-card-w)] shrink-0"
                />
              ))}
        </div>
      </div>
    </section>
  );
};

export default SitesSection;
