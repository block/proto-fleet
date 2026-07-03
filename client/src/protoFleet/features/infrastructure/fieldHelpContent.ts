import type { FieldHelpPopoverProps } from "@/protoFleet/features/infrastructure/fieldHelp";

export const infraDeviceFieldHelp: Record<
  "connectionType" | "deviceIdentifier" | "endpoint" | "port",
  FieldHelpPopoverProps
> = {
  connectionType: {
    ariaLabel: "About connection type",
    header: "Connection type",
    body: "Choose how Fleet reaches this device: Modbus TCP, MQTT bridge, or HTTP/API.",
    testId: "infra-device-connection-type-help",
  },
  deviceIdentifier: {
    ariaLabel: "About device identifier",
    header: "Device identifier",
    body: "Use the stable identifier reported by the controller, bridge, or integration for this infrastructure device.",
    testId: "infra-device-identifier-help",
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
