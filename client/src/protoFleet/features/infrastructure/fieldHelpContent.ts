import type { FieldHelpPopoverProps } from "@/protoFleet/features/infrastructure/fieldHelp";

export const infraDeviceFieldHelp: Record<"unitId" | "endpoint" | "port", FieldHelpPopoverProps> = {
  unitId: {
    ariaLabel: "About Unit ID",
    header: "Unit ID",
    body: "Numeric Modbus unit/slave address from 1 to 247 for this device at the configured endpoint.",
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
