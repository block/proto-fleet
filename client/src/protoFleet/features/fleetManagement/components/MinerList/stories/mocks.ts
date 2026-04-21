import { type Measurement } from "@/protoFleet/api/generated/common/v1/measurement_pb";
import {
  DeviceStatus,
  type MinerStateSnapshot,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { TemperatureStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

export const miners: MinerStateSnapshot[] = [
  {
    $typeName: "fleetmanagement.v1.MinerStateSnapshot",
    deviceIdentifier: "uuid:123456789",
    serialNumber: "123456789",
    name: "C1-M01",
    ipAddress: "0123456789",
    macAddress: "0a:04:8a:54:fa:9f",
    url: "https://0123456789:8080",
    pairingStatus: PairingStatus.PAIRED,
    model: "S19 Pro",
    manufacturer: "Bitmain",
    workerName: "worker-01",
    driverName: "antminer",
    hashrate: [
      {
        timestamp: { seconds: BigInt(1641024000), nanos: 0 },
        value: 189,
      } as Measurement,
      {
        timestamp: { seconds: BigInt(1641110400), nanos: 0 },
        value: 194,
      } as Measurement,
      {
        timestamp: { seconds: BigInt(1641196800), nanos: 0 },
        value: 190,
      } as Measurement,
      {
        timestamp: { seconds: BigInt(1641283200), nanos: 0 },
        value: 213.2,
      } as Measurement,
    ],
    efficiency: [
      {
        timestamp: { seconds: BigInt(2), nanos: 0 },
        value: 15.5,
      } as Measurement,
    ],
    powerUsage: [
      {
        timestamp: { seconds: BigInt(2), nanos: 0 },
        value: 3.5,
      } as Measurement,
    ],
    temperature: [
      {
        timestamp: { seconds: BigInt(2), nanos: 0 },
        value: 65.5,
      } as Measurement,
    ],
    deviceStatus: DeviceStatus.ONLINE,
    temperatureStatus: TemperatureStatus.OK,
    firmwareVersion: "2.0.0",
    groupLabels: [],
    rackLabel: "",
    rackPosition: "",
  },
  {
    $typeName: "fleetmanagement.v1.MinerStateSnapshot",
    deviceIdentifier: "uuid:1234567890",
    serialNumber: "123456780",
    name: "C1-M02",
    macAddress: "0b:04:8a:54:fa:9f",
    ipAddress: "0123456781",
    url: "https://0123456781:8080",
    pairingStatus: PairingStatus.PAIRED,
    model: "S19 Pro",
    manufacturer: "Bitmain",
    workerName: "worker-02",
    driverName: "antminer",
    hashrate: [
      {
        timestamp: { seconds: BigInt(1641024000), nanos: 0 },
        value: 160,
      } as Measurement,
      {
        timestamp: { seconds: BigInt(1641110400), nanos: 0 },
        value: 163,
      } as Measurement,
      {
        timestamp: { seconds: BigInt(1641196800), nanos: 0 },
        value: 165,
      } as Measurement,
      {
        timestamp: { seconds: BigInt(1641283200), nanos: 0 },
        value: 150.8,
      } as Measurement,
    ],
    efficiency: [
      {
        timestamp: { seconds: BigInt(2), nanos: 0 },
        value: 15.5,
      } as Measurement,
    ],
    powerUsage: [
      {
        timestamp: { seconds: BigInt(2), nanos: 0 },
        value: 3.5,
      } as Measurement,
    ],
    temperature: [
      {
        timestamp: { seconds: BigInt(2), nanos: 0 },
        value: 65.5,
      } as Measurement,
    ],
    deviceStatus: DeviceStatus.ONLINE,
    temperatureStatus: TemperatureStatus.OK,
    firmwareVersion: "2.0.0",
    groupLabels: [],
    rackLabel: "",
    rackPosition: "",
  },
  {
    $typeName: "fleetmanagement.v1.MinerStateSnapshot",
    deviceIdentifier: "uuid:123456781",
    serialNumber: "123456781",
    ipAddress: "172.27.244.166",
    name: "C1-M03",
    macAddress: "0c:04:8a:54:fa:9f",
    url: "https://172.27.244.166:8080",
    pairingStatus: PairingStatus.PAIRED,
    model: "S19 Pro",
    manufacturer: "Bitmain",
    workerName: "worker-03",
    driverName: "antminer",
    hashrate: [
      {
        timestamp: { seconds: BigInt(1641024000), nanos: 0 },
        value: 184,
      } as Measurement,
      {
        timestamp: { seconds: BigInt(1641110400), nanos: 0 },
        value: 196,
      } as Measurement,
      {
        timestamp: { seconds: BigInt(1641196800), nanos: 0 },
        value: 194,
      } as Measurement,
      {
        timestamp: { seconds: BigInt(1641283200), nanos: 0 },
        value: 187,
      } as Measurement,
    ],
    efficiency: [
      {
        timestamp: { seconds: BigInt(2), nanos: 0 },
        value: 15.5,
      } as Measurement,
    ],
    powerUsage: [
      {
        timestamp: { seconds: BigInt(2), nanos: 0 },
        value: 3.5,
      } as Measurement,
    ],
    temperature: [
      {
        timestamp: { seconds: BigInt(2), nanos: 0 },
        value: 65.5,
      } as Measurement,
    ],
    deviceStatus: DeviceStatus.ONLINE,
    temperatureStatus: TemperatureStatus.OK,
    firmwareVersion: "2.0.0",
    groupLabels: [],
    rackLabel: "",
    rackPosition: "",
  },
  {
    $typeName: "fleetmanagement.v1.MinerStateSnapshot",
    deviceIdentifier: "uuid:123456782",
    serialNumber: "123456782",
    ipAddress: "172.27.244.166",
    name: "C1-M04",
    macAddress: "0e:04:8a:54:fa:9f",
    url: "https://172.27.244.166:8080",
    pairingStatus: PairingStatus.PAIRED,
    model: "S19 Pro",
    manufacturer: "Bitmain",
    workerName: "worker-04",
    driverName: "antminer",
    hashrate: [
      {
        timestamp: { seconds: BigInt(1641024000), nanos: 0 },
        value: 184,
      } as Measurement,
      {
        timestamp: { seconds: BigInt(1641110400), nanos: 0 },
        value: 196,
      } as Measurement,
      {
        timestamp: { seconds: BigInt(1641196800), nanos: 0 },
        value: 194,
      } as Measurement,
      {
        timestamp: { seconds: BigInt(1641283200), nanos: 0 },
        value: 152.3,
      } as Measurement,
    ],
    efficiency: [
      {
        timestamp: { seconds: BigInt(2), nanos: 0 },
        value: 15.5,
      } as Measurement,
    ],
    powerUsage: [
      {
        timestamp: { seconds: BigInt(2), nanos: 0 },
        value: 3.5,
      } as Measurement,
    ],
    temperature: [
      {
        timestamp: { seconds: BigInt(2), nanos: 0 },
        value: 65.5,
      } as Measurement,
    ],
    deviceStatus: DeviceStatus.ONLINE,
    temperatureStatus: TemperatureStatus.OK,
    firmwareVersion: "2.0.0",
    groupLabels: [],
    rackLabel: "",
    rackPosition: "",
  },
];
