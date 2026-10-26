import { useState } from "react";
import MiningGroupsDialog from "./MiningGroupsDialog";
import AuthenticateFleetModal from "@/protoFleet/features/auth/components/AuthenticateFleetModal";

export default function EditHashboardPoolDialog(props: {
  deviceIdentifier: string;
  moduleIds?: bigint[];
  hashboardIds?: bigint[];
  onClose: () => void;
}) {
  const [credentials, setCredentials] = useState<{ username: string; password: string }>();

  if (!credentials)
    return (
      <AuthenticateFleetModal
        open
        purpose="pool"
        onAuthenticated={(username, password) => setCredentials({ username, password })}
        onDismiss={props.onClose}
      />
    );

  return <MiningGroupsDialog {...props} credentials={credentials} />;
}
