import { useState } from "react";

import StatusCircle from "@/shared/components/StatusCircle";

interface InfraDeviceSummary {
  id: string;
  name: string;
  status: string;
  deviceType: string;
}

const statusCircleStatus = (status: string) => {
  switch (status) {
    case "online":
      return "healthy" as const;
    case "degraded":
      return "warning" as const;
    case "offline":
      return "offline" as const;
    default:
      return "offline" as const;
  }
};

interface BuildingInfraPreviewProps {
  buildingId: bigint;
}

const BuildingInfraPreview = ({ buildingId }: BuildingInfraPreviewProps) => {
  const [devices] = useState<InfraDeviceSummary[]>([]);

  if (devices.length === 0) return null;

  const online = devices.filter((d) => d.status === "online").length;

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <span className="text-emphasis-300 font-medium">
          Infrastructure ({devices.length})
        </span>
        <span className="text-300 text-text-primary-70">
          {online} online
        </span>
      </div>
      <div className="flex flex-col gap-1">
        {devices.slice(0, 5).map((device) => (
          <div key={device.id} className="flex items-center gap-2 py-1">
            <StatusCircle status={statusCircleStatus(device.status)} />
            <span className="text-300 flex-1">{device.name}</span>
            <span className="text-200 text-text-primary-70 capitalize">{device.deviceType}</span>
          </div>
        ))}
        {devices.length > 5 && (
          <span className="text-300 text-text-primary-70">
            +{devices.length - 5} more
          </span>
        )}
      </div>
    </div>
  );
};

export default BuildingInfraPreview;
