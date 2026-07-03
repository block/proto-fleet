import type { FieldHelpPopoverProps } from "@/protoFleet/features/infrastructure/fieldHelp";

export const infraDeviceFieldHelp: Record<"connectionType" | "unitId" | "endpoint" | "port", FieldHelpPopoverProps> = {
  connectionType: {
    ariaLabel: "About connection type",
    header: "Connection type",
    body: "Modbus TCP is the only infrastructure device connection type supported in v1.",
    testId: "infra-device-connection-type-help",
  },
  unitId: {
    ariaLabel: "About Unit ID",
    header: "Unit ID",
    body: "Numeric Modbus unit/slave address for this device at the configured endpoint.",
    testId: "infra-device-unit-id-help",
  },
  endpoint: {
    ariaLabel: "About endpoint",
    header: "Endpoint",
    body: "Use the device IP address or DNS hostname Fleet should connect to.",
    testId: "infra-device-endpoint-help",
  },
  port: {
    ariaLabel: "About port",
    header: "Port",
    body: "Use the Modbus TCP port, such as 502.",
    testId: "infra-device-port-help",
  },
};
