import type { HomeTab } from "../types";
import Button from "@/shared/components/Button";
import DurationSelector, { type Duration } from "@/shared/components/DurationSelector";

interface Props {
  tab: HomeTab;
  links: Record<HomeTab, string>;
  duration: Duration;
  onDurationChange: (duration: Duration) => void;
}

const tabs: { id: HomeTab; label: string }[] = [
  { id: "overview", label: "Overview" },
  { id: "hashrate", label: "Hashrate" },
  { id: "efficiency", label: "Efficiency" },
  { id: "power", label: "Power" },
  { id: "temperature", label: "Temperature" },
];

export default function ContainerHomeToolbar({ tab, links, duration, onDurationChange }: Props) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-4">
      <nav aria-label="Container Home" className="flex flex-wrap gap-1">
        {tabs.map(({ id, label }) => (
          <Button
            key={id}
            variant={tab === id ? "primary" : "secondary"}
            size="compact"
            text={label}
            to={links[id]}
            ariaCurrent={tab === id ? "page" : undefined}
          />
        ))}
      </nav>
      {tab === "overview" ? null : <DurationSelector duration={duration} onSelect={onDurationChange} />}
    </div>
  );
}
