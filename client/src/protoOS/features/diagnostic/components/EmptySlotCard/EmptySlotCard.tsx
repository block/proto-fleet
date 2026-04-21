import Card from "@/protoOS/features/diagnostic/components/Card";
import CardHeader from "@/protoOS/features/diagnostic/components/CardHeader";
import {
  FanIndicatorV2 as FanIndicator,
  HashboardIndicatorV2 as HashboardIndicator,
  PsuIndicatorV2 as PsuIndicator,
} from "@/shared/assets/icons";

interface EmptySlotCardProps {
  type: "fan" | "hashboard" | "psu";
  position: number;
  title: string;
}

function EmptySlotCard({ type, position, title }: EmptySlotCardProps) {
  const getComponentIcon = () => {
    switch (type) {
      case "fan":
        return <FanIndicator position={position} />;
      case "hashboard":
        return <HashboardIndicator width="w-4" position={position} />;
      case "psu":
        return <PsuIndicator position={position} />;
    }
  };

  return (
    <Card>
      <CardHeader title={title} componentIcon={getComponentIcon()} />
      <div className="flex items-center justify-center py-8">
        <p className="text-300 text-text-primary-70">No {type} detected in this slot</p>
      </div>
    </Card>
  );
}

export default EmptySlotCard;
