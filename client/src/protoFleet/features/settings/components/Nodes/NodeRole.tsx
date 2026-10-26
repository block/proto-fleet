import {
  FleetNodeEnrollmentStatus,
  FleetNodeHARole,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";

const NodeRole = ({ node }: { node: FleetNodeItem }) => {
  let label = "Standalone";
  if (node.haRole === FleetNodeHARole.FLEET_NODE_HA_ROLE_ACTIVE) {
    label = "Active";
  } else if (node.haRole === FleetNodeHARole.FLEET_NODE_HA_ROLE_STANDBY) {
    label =
      node.enrollmentStatus === FleetNodeEnrollmentStatus.CONFIRMED && !node.haStandbyReady ? "Preparing" : "Standby";
  }
  return <span title={node.haStatusReason || undefined}>{label}</span>;
};

export default NodeRole;
