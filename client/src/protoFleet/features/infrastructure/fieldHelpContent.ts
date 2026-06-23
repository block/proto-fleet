import type { FieldHelpPopoverProps } from "@/protoFleet/features/infrastructure/fieldHelp";

export const infraDeviceFieldHelp: Record<"connectionType" | "endpoint" | "port", FieldHelpPopoverProps> = {
  connectionType: {
    ariaLabel: "About connection type",
    header: "Connection type",
    body: "Choose how Fleet reaches this device: Modbus TCP, MQTT bridge, or HTTP/API.",
    testId: "infra-device-connection-type-help",
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
    body: "Use the TCP port for the selected connection type, such as 502 for Modbus TCP.",
    testId: "infra-device-port-help",
  },
};
