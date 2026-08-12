import { useState } from "react";
import FanCard from "./FanCard";
import RadialGauge from "@/shared/components/RadialGauge";

export default {
  title: "Proto Fleet/Containers/Fan Gauge (proof)",
};

// Bare gauge in a range of states + a warning color, on a card-like surface.
export const Gauge = () => (
  <div className="flex flex-wrap gap-6 bg-surface-base p-8">
    {[
      { v: 0, label: "0", cap: "%", color: "text-core-primary-20" },
      { v: 45, label: "45", cap: "%", color: "text-core-accent-fill" },
      { v: 82, label: "82", cap: "%", color: "text-intent-warning-fill" },
      { v: 100, label: "100", cap: "%", color: "text-core-accent-fill" },
    ].map((g) => (
      <RadialGauge
        key={g.v}
        value={g.v}
        sweep={270}
        size={112}
        colorClassName={g.color}
        label={g.label}
        caption={g.cap}
      />
    ))}
  </div>
);

// Row of interactive fan cards (toggle flips the gauge on/off), as they'd sit
// on the container overview.
export const FanRow = () => {
  const initial = [
    { label: "Fan 1", speedPercent: 68, speedLabel: "3,200", on: true },
    { label: "Fan 2", speedPercent: 74, speedLabel: "3,480", on: true },
    { label: "Fan 3", speedPercent: 61, speedLabel: "2,870", on: true },
    { label: "Fan 4", speedPercent: 0, speedLabel: "0", on: false },
    { label: "Fan 5", speedPercent: 71, speedLabel: "3,340", on: true },
    { label: "Fan 6", speedPercent: 66, speedLabel: "3,100", on: true },
    { label: "Fan 7", speedPercent: 79, speedLabel: "3,710", on: true },
    { label: "Fan 8", speedPercent: 58, speedLabel: "2,720", on: true },
    { label: "Fan 9", speedPercent: 72, speedLabel: "3,380", on: true },
    { label: "Fan 10", speedPercent: 64, speedLabel: "3,010", on: true },
  ];
  const [fans, setFans] = useState(initial);

  return (
    <div className="bg-surface-base p-8">
      <div className="grid grid-cols-2 gap-3 tablet:grid-cols-3 laptop:grid-cols-5 phone:grid-cols-1">
        {fans.map((fan, i) => (
          <FanCard
            key={fan.label}
            label={fan.label}
            speedPercent={fan.speedPercent}
            speedLabel={fan.speedLabel}
            on={fan.on}
            onToggle={(on) => setFans((prev) => prev.map((f, idx) => (idx === i ? { ...f, on } : f)))}
            onInfo={() => {}}
          />
        ))}
      </div>
    </div>
  );
};
