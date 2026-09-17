import type { ReactNode } from "react";
import type { HomeHistory, HomeReading, LocationReading } from "../types";
import { HomeHistoryChart, LocationBarChart } from "./HomeCharts";
import Button from "@/shared/components/Button";
import Card, { cardType } from "@/shared/components/Card";
import Stat from "@/shared/components/Stat";

function OverviewCard({
  reading,
  status,
  to,
  children,
}: {
  reading: HomeReading;
  status?: string;
  to: string;
  children: ReactNode;
}) {
  return (
    <Card
      type={cardType.default}
      headerTone="neutral"
      className="min-w-0"
      headerClassName="items-start px-6 pt-6 pb-0"
      bodyClassName="p-6 pt-0"
      title={<Stat label={reading.label} value={reading.value} size="medium" />}
      headerAction={
        <div className="flex flex-wrap items-center justify-end gap-3 text-200 text-text-primary-50">
          {status ? <span>{status}</span> : null}
          <Button
            variant="textOnly"
            text="View"
            to={to}
            ariaLabel={`View ${reading.label}`}
            textColor="text-text-primary"
            textOnlyUnderlineOnHover={false}
            className="underline"
          />
        </div>
      }
    >
      {children}
    </Card>
  );
}

interface Props {
  hashrate: HomeReading;
  power: HomeReading;
  fluid: HomeReading;
  boardCount?: string;
  history: HomeHistory;
  powerLocations: LocationReading[];
  fluidLocations: LocationReading[];
  links: { hashrate: string; power: string; fluid: string };
}

export default function ContainerOverviewView({
  hashrate,
  power,
  fluid,
  boardCount,
  history,
  powerLocations,
  fluidLocations,
  links,
}: Props) {
  return (
    <div className="flex flex-col gap-1">
      <h1 className="sr-only">Container overview</h1>
      <OverviewCard reading={hashrate} status={boardCount} to={links.hashrate}>
        <HomeHistoryChart history={history} compact />
      </OverviewCard>
      <div className="grid grid-cols-1 gap-1 tablet:grid-cols-2">
        <OverviewCard reading={power} to={links.power}>
          <LocationBarChart readings={powerLocations} color="var(--color-core-primary-fill)" />
        </OverviewCard>
        <OverviewCard reading={fluid} to={links.fluid}>
          <LocationBarChart readings={fluidLocations} />
        </OverviewCard>
      </div>
    </div>
  );
}
