"""Miner error taxonomy and device error types.

This module defines the standardized error codes for mining device errors, severity
levels, component types, and device error structures.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime
from enum import IntEnum

__all__ = [
    "MinerError",
    "Severity",
    "ComponentType",
    "DeviceError",
    "DeviceErrors",
]


class MinerError(IntEnum):
    """Standardized miner error codes.

    Error code ranges:
    - 1000-1499: PSU & facility power
    - 2000-2499: Thermal & fans
    - 3000-3499: Hashboard / ASIC chain & core digital
    - 3500-3699: Board-level power rails & protection
    - 4000-4499: Sensors (electrical faults)
    - 5000-5499: Non-volatile storage / firmware
    - 6000-6499: Control-plane & on-board comms
    - 8000-8499: Performance advisories (non-fatal)
    - 9000-9999: Catch-alls
    """

    MINER_ERROR_UNSPECIFIED = 0

    # 1000–1499: PSU (unit-level faults)
    PSU_NOT_PRESENT = 1000
    PSU_MODEL_MISMATCH = 1001
    PSU_COMMUNICATION_LOST = 1002
    PSU_FAULT_GENERIC = 1003
    PSU_INPUT_VOLTAGE_LOW = 1004
    PSU_INPUT_VOLTAGE_HIGH = 1005
    PSU_OUTPUT_VOLTAGE_FAULT = 1006
    PSU_OUTPUT_OVERCURRENT = 1007
    PSU_FAN_FAULT = 1008
    PSU_OVER_TEMPERATURE = 1009
    PSU_INPUT_PHASE_IMBALANCE = 1010
    PSU_UNDER_TEMPERATURE = 1011

    # 2000–2499: Thermal & fans (device-level)
    FAN_FAILED = 2000
    FAN_TACH_SIGNAL_LOST = 2001
    FAN_SPEED_DEVIATION = 2002
    INLET_OVER_TEMPERATURE = 2010
    DEVICE_OVER_TEMPERATURE = 2011
    DEVICE_UNDER_TEMPERATURE = 2012

    # 3000–3499: Hashboard / ASIC chain & core digital
    HASHBOARD_NOT_PRESENT = 3000
    HASHBOARD_OVER_TEMPERATURE = 3001
    HASHBOARD_MISSING_CHIPS = 3002
    ASIC_CHAIN_COMMUNICATION_LOST = 3003
    ASIC_CLOCK_PLL_UNLOCKED = 3004
    ASIC_CRC_ERROR_EXCESSIVE = 3005
    HASHBOARD_ASIC_OVER_TEMPERATURE = 3006
    HASHBOARD_ASIC_UNDER_TEMPERATURE = 3007

    # 3500–3699: Board-level power rails & protection
    BOARD_POWER_PGOOD_MISSING = 3500
    BOARD_POWER_OVERCURRENT = 3501
    BOARD_POWER_RAIL_UNDERVOLT = 3502
    BOARD_POWER_RAIL_OVERVOLT = 3503
    BOARD_POWER_SHORT_DETECTED = 3504

    # 4000–4499: Sensors (electrical faults)
    TEMP_SENSOR_OPEN_OR_SHORT = 4000
    TEMP_SENSOR_FAULT = 4001
    VOLTAGE_SENSOR_FAULT = 4002
    CURRENT_SENSOR_FAULT = 4003

    # 5000–5499: Non-volatile storage / firmware
    EEPROM_CRC_MISMATCH = 5000
    EEPROM_READ_FAILURE = 5001
    FIRMWARE_IMAGE_INVALID = 5002
    FIRMWARE_CONFIG_INVALID = 5003

    # 6000–6499: Control-plane & on-board comms
    CONTROL_BOARD_COMMUNICATION_LOST = 6000
    CONTROL_BOARD_FAILURE = 6001
    DEVICE_INTERNAL_BUS_FAULT = 6002
    DEVICE_COMMUNICATION_LOST = 6003
    IO_MODULE_FAILURE = 6010

    # 8000–8499: Performance advisories (non-fatal)
    HASHRATE_BELOW_TARGET = 8000
    HASHBOARD_WARN_CRC_HIGH = 8001
    THERMAL_MARGIN_LOW = 8002

    # 9000–9999: Catch-alls
    VENDOR_ERROR_UNMAPPED = 9000


class Severity(IntEnum):
    """Error severity levels."""

    SEVERITY_UNSPECIFIED = 0
    SEVERITY_CRITICAL = 1  # Miner stops hashing or unsafe
    SEVERITY_MAJOR = 2  # Degraded hashing / imminent trip
    SEVERITY_MINOR = 3  # Recoverable, limited effect
    SEVERITY_INFO = 4  # Informational / advisory


class ComponentType(IntEnum):
    """Hardware component types."""

    COMPONENT_TYPE_UNSPECIFIED = 0
    COMPONENT_TYPE_PSU = 1
    COMPONENT_TYPE_HASH_BOARD = 2
    COMPONENT_TYPE_FAN = 3
    COMPONENT_TYPE_CONTROL_BOARD = 4
    COMPONENT_TYPE_EEPROM = 5
    COMPONENT_TYPE_IO_MODULE = 6


@dataclass(frozen=True)
class DeviceError:
    """A miner error reported by a device."""

    miner_error: MinerError
    cause_summary: str
    recommended_action: str
    severity: Severity
    first_seen_at: datetime
    last_seen_at: datetime
    device_id: str
    summary: str
    component_type: ComponentType
    closed_at: datetime | None = None
    vendor_attributes: dict[str, str] = field(default_factory=dict)
    component_id: str | None = None
    impact: str = ""

    def is_active(self) -> bool:
        return self.closed_at is None

    def is_critical(self) -> bool:
        return self.severity == Severity.SEVERITY_CRITICAL


@dataclass(frozen=True)
class DeviceErrors:
    """Collection of errors for a specific device."""

    device_id: str
    errors: tuple[DeviceError, ...]

    def active_errors(self) -> list[DeviceError]:
        return [err for err in self.errors if err.is_active()]

    def critical_errors(self) -> list[DeviceError]:
        return [err for err in self.errors if err.is_critical()]

    def has_critical_errors(self) -> bool:
        return any(err.is_critical() for err in self.errors)
