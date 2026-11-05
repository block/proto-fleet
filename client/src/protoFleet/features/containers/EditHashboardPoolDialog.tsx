import { ConfigurationSelector } from "@/protoFleet/features/poolConfigurations/ConfigurationSelector";

export default function EditHashboardPoolDialog({
  moduleIds,
  hashboardIds,
  onClose,
}: {
  deviceIdentifier: string;
  moduleIds?: bigint[];
  hashboardIds?: bigint[];
  onClose: () => void;
}) {
  return <ConfigurationSelector boards={{ moduleIds, hashboardIds }} onDismiss={onClose} />;
}
