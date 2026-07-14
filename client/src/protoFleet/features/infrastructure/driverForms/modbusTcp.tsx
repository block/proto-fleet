import type {
  DriverFormFieldsProps,
  DriverFormModule,
  DriverFormValues,
} from "@/protoFleet/features/infrastructure/driverForms/types";
/* eslint-disable react-refresh/only-export-components -- the form fields component ships inside the driver form module object by design; not HMR-relevant */
import { FieldHelpPopover, type FieldHelpPopoverProps } from "@/protoFleet/features/infrastructure/fieldHelp";
import Input from "@/shared/components/Input";
import Select from "@/shared/components/Select";

const MIN_UNIT_ID = 1;
const MAX_UNIT_ID = 247;
const MIN_PORT = 1;
const MAX_PORT = 65535;
const MIN_REGISTER_ADDRESS = 0;
const MAX_REGISTER_ADDRESS = 65535;

const WRITE_MODE_COIL = "coil";
const WRITE_MODE_HOLDING_REGISTER = "holding_register";

const writeModeOptions = [
  { value: WRITE_MODE_COIL, label: "Coil" },
  { value: WRITE_MODE_HOLDING_REGISTER, label: "Holding register" },
];

const writeModeLabels: Record<string, string> = {
  [WRITE_MODE_COIL]: "coil",
  [WRITE_MODE_HOLDING_REGISTER]: "holding register",
};

const fieldHelp: Record<"unitId" | "endpoint" | "port" | "registerAddress" | "writeMode", FieldHelpPopoverProps> = {
  unitId: {
    ariaLabel: "About Unit ID",
    header: "Unit ID",
    body: "Numeric Modbus unit/slave address from 1 to 247 for this device at the configured endpoint.",
    testId: "infra-device-unit-id-help",
  },
  endpoint: {
    ariaLabel: "About endpoint",
    header: "Endpoint",
    body: "Private IP address of the drive or PLC (RFC1918 or IPv6 ULA). Hostnames, loopback, and public addresses are not accepted.",
    testId: "infra-device-endpoint-help",
  },
  port: {
    ariaLabel: "About port",
    header: "Port",
    body: "Use the Modbus TCP port, such as 502.",
    testId: "infra-device-port-help",
  },
  registerAddress: {
    ariaLabel: "About register address",
    header: "Register address",
    body: "Raw application address of the run/stop target from 0 to 65535, e.g. 2001 for a drive control word or 1 for a RUN/STOP coil. Do not use the 4xxxx-prefixed convention (e.g. 42001) — enter the plain address; Fleet handles the wire-level off-by-one translation.",
    testId: "infra-device-register-address-help",
  },
  writeMode: {
    ariaLabel: "About write mode",
    header: "Write mode",
    body: "Coil writes the RUN/STOP coil (function code 5). Holding register writes 0/1 to a control word register (function code 6). Use whichever target the site's drive or PLC integration expects.",
    testId: "infra-device-write-mode-help",
  },
};

const isIntegerInRange = (value: string, min: number, max: number) => {
  const trimmed = value.trim();
  if (!/^\d+$/.test(trimmed)) return false;
  const parsed = Number(trimmed);
  return Number.isSafeInteger(parsed) && parsed >= min && parsed <= max;
};

const emptyValues = (): DriverFormValues => ({
  endpoint: "",
  port: "",
  unitId: "",
  registerAddress: "",
  writeMode: WRITE_MODE_COIL,
});

const decode = (driverConfig: string): DriverFormValues | null => {
  if (!driverConfig.trim()) return null;

  let parsed: unknown;
  try {
    parsed = JSON.parse(driverConfig);
  } catch {
    return null;
  }
  if (typeof parsed !== "object" || parsed === null) return null;

  const config = parsed as Record<string, unknown>;
  const asString = (value: unknown) =>
    typeof value === "string" ? value : typeof value === "number" ? String(value) : "";

  return {
    endpoint: asString(config.endpoint),
    port: asString(config.port),
    unitId: asString(config.unit_id),
    registerAddress: asString(config.register_address),
    writeMode: asString(config.write_mode) || WRITE_MODE_COIL,
  };
};

const isValid = (values: DriverFormValues): boolean =>
  values.endpoint.trim().length > 0 &&
  isIntegerInRange(values.port, MIN_PORT, MAX_PORT) &&
  isIntegerInRange(values.unitId, MIN_UNIT_ID, MAX_UNIT_ID) &&
  isIntegerInRange(values.registerAddress, MIN_REGISTER_ADDRESS, MAX_REGISTER_ADDRESS) &&
  (values.writeMode === WRITE_MODE_COIL || values.writeMode === WRITE_MODE_HOLDING_REGISTER);

const encode = (values: DriverFormValues): string =>
  JSON.stringify({
    endpoint: values.endpoint.trim(),
    port: Number(values.port.trim()),
    unit_id: Number(values.unitId.trim()),
    register_address: Number(values.registerAddress.trim()),
    write_mode: values.writeMode,
  });

const summarize = (driverConfig: string): string | null => {
  const values = decode(driverConfig);
  if (!values || !values.endpoint) return null;

  const parts = [`${values.endpoint}${values.port ? `:${values.port}` : ""}`];
  if (values.unitId) parts.push(`unit ${values.unitId}`);
  if (values.registerAddress) {
    parts.push(`${writeModeLabels[values.writeMode] ?? values.writeMode} ${values.registerAddress}`);
  }
  return parts.join(" · ");
};

const FormFields = ({ idPrefix, values, onChange, disabled = false }: DriverFormFieldsProps) => (
  <div className="flex flex-col gap-4">
    <div className="grid grid-cols-2 gap-3">
      <Input
        id={`${idPrefix}-endpoint`}
        label="Endpoint"
        initValue={values.endpoint}
        readOnly={disabled}
        suffixAction={<FieldHelpPopover {...fieldHelp.endpoint} />}
        onChange={(value) => onChange("endpoint", value)}
      />
      <Input
        id={`${idPrefix}-port`}
        label="Port"
        type="number"
        inputMode="numeric"
        initValue={values.port}
        readOnly={disabled}
        suffixAction={<FieldHelpPopover {...fieldHelp.port} />}
        onChange={(value) => onChange("port", value)}
      />
    </div>
    <div className="grid grid-cols-2 gap-3">
      <Input
        id={`${idPrefix}-unit-id`}
        label="Unit ID"
        type="number"
        inputMode="numeric"
        initValue={values.unitId}
        readOnly={disabled}
        suffixAction={<FieldHelpPopover {...fieldHelp.unitId} />}
        onChange={(value) => onChange("unitId", value)}
      />
      <Select
        id={`${idPrefix}-write-mode`}
        label="Write mode"
        options={writeModeOptions}
        value={values.writeMode}
        onChange={(value) => onChange("writeMode", value)}
        disabled={disabled}
        forceBelow
      />
    </div>
    <Input
      id={`${idPrefix}-register-address`}
      label="Register address"
      type="number"
      inputMode="numeric"
      initValue={values.registerAddress}
      readOnly={disabled}
      suffixAction={<FieldHelpPopover {...fieldHelp.registerAddress} />}
      onChange={(value) => onChange("registerAddress", value)}
    />
  </div>
);

export const modbusTcpFormModule: DriverFormModule = {
  driverType: "modbus_tcp",
  label: "Modbus TCP",
  emptyValues,
  decode,
  isValid,
  encode,
  summarize,
  FormFields,
};
