import { Link } from "react-router-dom";

import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";

// FPO implementation. Phase 1a ships this card as a grey box with label +
// rack count so the navigation flow from /sites → /buildings/:id is wired
// end-to-end. #263 replaces the body with the real BuildingCard (visuals +
// per-building metrics + health indicators) — the file path stays the same
// so imports across /sites don't churn during the swap.

interface BuildingCardProps {
  building: BuildingWithCounts;
}

const BuildingCard = ({ building }: BuildingCardProps) => {
  const id = (building.building?.id ?? 0n).toString();
  const label = building.building?.name ?? "(unnamed building)";
  const rackCount = building.rackCount.toString();

  return (
    <Link
      to={`/buildings/${id}`}
      className="hover:bg-surface-base-hover block rounded-xl border border-border-5 bg-surface-base p-4"
      data-testid={`building-card-${id}`}
    >
      <div className="text-emphasis-300">{label}</div>
      <div className="text-300 text-text-primary-70">{rackCount} racks / — miners</div>
    </Link>
  );
};

export default BuildingCard;
