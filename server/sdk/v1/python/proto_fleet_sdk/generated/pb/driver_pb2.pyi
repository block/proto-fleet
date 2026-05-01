from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf import duration_pb2 as _duration_pb2
from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class HealthStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    HEALTH_STATUS_UNSPECIFIED: _ClassVar[HealthStatus]
    HEALTH_UNKNOWN: _ClassVar[HealthStatus]
    HEALTH_HEALTHY_ACTIVE: _ClassVar[HealthStatus]
    HEALTH_HEALTHY_INACTIVE: _ClassVar[HealthStatus]
    HEALTH_WARNING: _ClassVar[HealthStatus]
    HEALTH_CRITICAL: _ClassVar[HealthStatus]

class ComponentStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    COMPONENT_STATUS_UNSPECIFIED: _ClassVar[ComponentStatus]
    COMPONENT_STATUS_UNKNOWN: _ClassVar[ComponentStatus]
    COMPONENT_STATUS_HEALTHY: _ClassVar[ComponentStatus]
    COMPONENT_STATUS_WARNING: _ClassVar[ComponentStatus]
    COMPONENT_STATUS_CRITICAL: _ClassVar[ComponentStatus]
    COMPONENT_STATUS_OFFLINE: _ClassVar[ComponentStatus]
    COMPONENT_STATUS_DISABLED: _ClassVar[ComponentStatus]

class MetricKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    METRIC_KIND_UNSPECIFIED: _ClassVar[MetricKind]
    METRIC_KIND_GAUGE: _ClassVar[MetricKind]
    METRIC_KIND_RATE: _ClassVar[MetricKind]
    METRIC_KIND_COUNTER: _ClassVar[MetricKind]

class CoolingMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    COOLING_MODE_UNSPECIFIED: _ClassVar[CoolingMode]
    COOLING_MODE_AIR_COOLED: _ClassVar[CoolingMode]
    COOLING_MODE_IMMERSION_COOLED: _ClassVar[CoolingMode]
    COOLING_MODE_MANUAL: _ClassVar[CoolingMode]

class PerformanceMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PERFORMANCE_MODE_UNSPECIFIED: _ClassVar[PerformanceMode]
    PERFORMANCE_MODE_MAXIMUM_HASHRATE: _ClassVar[PerformanceMode]
    PERFORMANCE_MODE_EFFICIENCY: _ClassVar[PerformanceMode]

class CurtailLevel(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CURTAIL_LEVEL_UNSPECIFIED: _ClassVar[CurtailLevel]
    CURTAIL_LEVEL_EFFICIENCY: _ClassVar[CurtailLevel]
    CURTAIL_LEVEL_FULL: _ClassVar[CurtailLevel]

class MinerError(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    MINER_ERROR_UNSPECIFIED: _ClassVar[MinerError]
    PSU_NOT_PRESENT: _ClassVar[MinerError]
    PSU_MODEL_MISMATCH: _ClassVar[MinerError]
    PSU_COMMUNICATION_LOST: _ClassVar[MinerError]
    PSU_FAULT_GENERIC: _ClassVar[MinerError]
    PSU_INPUT_VOLTAGE_LOW: _ClassVar[MinerError]
    PSU_INPUT_VOLTAGE_HIGH: _ClassVar[MinerError]
    PSU_OUTPUT_VOLTAGE_FAULT: _ClassVar[MinerError]
    PSU_OUTPUT_OVERCURRENT: _ClassVar[MinerError]
    PSU_FAN_FAULT: _ClassVar[MinerError]
    PSU_OVER_TEMPERATURE: _ClassVar[MinerError]
    PSU_INPUT_PHASE_IMBALANCE: _ClassVar[MinerError]
    PSU_UNDER_TEMPERATURE: _ClassVar[MinerError]
    FAN_FAILED: _ClassVar[MinerError]
    FAN_TACH_SIGNAL_LOST: _ClassVar[MinerError]
    FAN_SPEED_DEVIATION: _ClassVar[MinerError]
    INLET_OVER_TEMPERATURE: _ClassVar[MinerError]
    DEVICE_OVER_TEMPERATURE: _ClassVar[MinerError]
    DEVICE_UNDER_TEMPERATURE: _ClassVar[MinerError]
    HASHBOARD_NOT_PRESENT: _ClassVar[MinerError]
    HASHBOARD_OVER_TEMPERATURE: _ClassVar[MinerError]
    HASHBOARD_MISSING_CHIPS: _ClassVar[MinerError]
    ASIC_CHAIN_COMMUNICATION_LOST: _ClassVar[MinerError]
    ASIC_CLOCK_PLL_UNLOCKED: _ClassVar[MinerError]
    ASIC_CRC_ERROR_EXCESSIVE: _ClassVar[MinerError]
    HASHBOARD_ASIC_OVER_TEMPERATURE: _ClassVar[MinerError]
    HASHBOARD_ASIC_UNDER_TEMPERATURE: _ClassVar[MinerError]
    BOARD_POWER_PGOOD_MISSING: _ClassVar[MinerError]
    BOARD_POWER_OVERCURRENT: _ClassVar[MinerError]
    BOARD_POWER_RAIL_UNDERVOLT: _ClassVar[MinerError]
    BOARD_POWER_RAIL_OVERVOLT: _ClassVar[MinerError]
    BOARD_POWER_SHORT_DETECTED: _ClassVar[MinerError]
    TEMP_SENSOR_OPEN_OR_SHORT: _ClassVar[MinerError]
    TEMP_SENSOR_FAULT: _ClassVar[MinerError]
    VOLTAGE_SENSOR_FAULT: _ClassVar[MinerError]
    CURRENT_SENSOR_FAULT: _ClassVar[MinerError]
    EEPROM_CRC_MISMATCH: _ClassVar[MinerError]
    EEPROM_READ_FAILURE: _ClassVar[MinerError]
    FIRMWARE_IMAGE_INVALID: _ClassVar[MinerError]
    FIRMWARE_CONFIG_INVALID: _ClassVar[MinerError]
    CONTROL_BOARD_COMMUNICATION_LOST: _ClassVar[MinerError]
    CONTROL_BOARD_FAILURE: _ClassVar[MinerError]
    DEVICE_INTERNAL_BUS_FAULT: _ClassVar[MinerError]
    DEVICE_COMMUNICATION_LOST: _ClassVar[MinerError]
    IO_MODULE_FAILURE: _ClassVar[MinerError]
    HASHRATE_BELOW_TARGET: _ClassVar[MinerError]
    HASHBOARD_WARN_CRC_HIGH: _ClassVar[MinerError]
    THERMAL_MARGIN_LOW: _ClassVar[MinerError]
    VENDOR_ERROR_UNMAPPED: _ClassVar[MinerError]

class Severity(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SEVERITY_UNSPECIFIED: _ClassVar[Severity]
    SEVERITY_CRITICAL: _ClassVar[Severity]
    SEVERITY_MAJOR: _ClassVar[Severity]
    SEVERITY_MINOR: _ClassVar[Severity]
    SEVERITY_INFO: _ClassVar[Severity]

class ComponentType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    COMPONENT_TYPE_UNSPECIFIED: _ClassVar[ComponentType]
    COMPONENT_TYPE_PSU: _ClassVar[ComponentType]
    COMPONENT_TYPE_HASH_BOARD: _ClassVar[ComponentType]
    COMPONENT_TYPE_FAN: _ClassVar[ComponentType]
    COMPONENT_TYPE_CONTROL_BOARD: _ClassVar[ComponentType]
    COMPONENT_TYPE_EEPROM: _ClassVar[ComponentType]
    COMPONENT_TYPE_IO_MODULE: _ClassVar[ComponentType]
HEALTH_STATUS_UNSPECIFIED: HealthStatus
HEALTH_UNKNOWN: HealthStatus
HEALTH_HEALTHY_ACTIVE: HealthStatus
HEALTH_HEALTHY_INACTIVE: HealthStatus
HEALTH_WARNING: HealthStatus
HEALTH_CRITICAL: HealthStatus
COMPONENT_STATUS_UNSPECIFIED: ComponentStatus
COMPONENT_STATUS_UNKNOWN: ComponentStatus
COMPONENT_STATUS_HEALTHY: ComponentStatus
COMPONENT_STATUS_WARNING: ComponentStatus
COMPONENT_STATUS_CRITICAL: ComponentStatus
COMPONENT_STATUS_OFFLINE: ComponentStatus
COMPONENT_STATUS_DISABLED: ComponentStatus
METRIC_KIND_UNSPECIFIED: MetricKind
METRIC_KIND_GAUGE: MetricKind
METRIC_KIND_RATE: MetricKind
METRIC_KIND_COUNTER: MetricKind
COOLING_MODE_UNSPECIFIED: CoolingMode
COOLING_MODE_AIR_COOLED: CoolingMode
COOLING_MODE_IMMERSION_COOLED: CoolingMode
COOLING_MODE_MANUAL: CoolingMode
PERFORMANCE_MODE_UNSPECIFIED: PerformanceMode
PERFORMANCE_MODE_MAXIMUM_HASHRATE: PerformanceMode
PERFORMANCE_MODE_EFFICIENCY: PerformanceMode
CURTAIL_LEVEL_UNSPECIFIED: CurtailLevel
CURTAIL_LEVEL_EFFICIENCY: CurtailLevel
CURTAIL_LEVEL_FULL: CurtailLevel
MINER_ERROR_UNSPECIFIED: MinerError
PSU_NOT_PRESENT: MinerError
PSU_MODEL_MISMATCH: MinerError
PSU_COMMUNICATION_LOST: MinerError
PSU_FAULT_GENERIC: MinerError
PSU_INPUT_VOLTAGE_LOW: MinerError
PSU_INPUT_VOLTAGE_HIGH: MinerError
PSU_OUTPUT_VOLTAGE_FAULT: MinerError
PSU_OUTPUT_OVERCURRENT: MinerError
PSU_FAN_FAULT: MinerError
PSU_OVER_TEMPERATURE: MinerError
PSU_INPUT_PHASE_IMBALANCE: MinerError
PSU_UNDER_TEMPERATURE: MinerError
FAN_FAILED: MinerError
FAN_TACH_SIGNAL_LOST: MinerError
FAN_SPEED_DEVIATION: MinerError
INLET_OVER_TEMPERATURE: MinerError
DEVICE_OVER_TEMPERATURE: MinerError
DEVICE_UNDER_TEMPERATURE: MinerError
HASHBOARD_NOT_PRESENT: MinerError
HASHBOARD_OVER_TEMPERATURE: MinerError
HASHBOARD_MISSING_CHIPS: MinerError
ASIC_CHAIN_COMMUNICATION_LOST: MinerError
ASIC_CLOCK_PLL_UNLOCKED: MinerError
ASIC_CRC_ERROR_EXCESSIVE: MinerError
HASHBOARD_ASIC_OVER_TEMPERATURE: MinerError
HASHBOARD_ASIC_UNDER_TEMPERATURE: MinerError
BOARD_POWER_PGOOD_MISSING: MinerError
BOARD_POWER_OVERCURRENT: MinerError
BOARD_POWER_RAIL_UNDERVOLT: MinerError
BOARD_POWER_RAIL_OVERVOLT: MinerError
BOARD_POWER_SHORT_DETECTED: MinerError
TEMP_SENSOR_OPEN_OR_SHORT: MinerError
TEMP_SENSOR_FAULT: MinerError
VOLTAGE_SENSOR_FAULT: MinerError
CURRENT_SENSOR_FAULT: MinerError
EEPROM_CRC_MISMATCH: MinerError
EEPROM_READ_FAILURE: MinerError
FIRMWARE_IMAGE_INVALID: MinerError
FIRMWARE_CONFIG_INVALID: MinerError
CONTROL_BOARD_COMMUNICATION_LOST: MinerError
CONTROL_BOARD_FAILURE: MinerError
DEVICE_INTERNAL_BUS_FAULT: MinerError
DEVICE_COMMUNICATION_LOST: MinerError
IO_MODULE_FAILURE: MinerError
HASHRATE_BELOW_TARGET: MinerError
HASHBOARD_WARN_CRC_HIGH: MinerError
THERMAL_MARGIN_LOW: MinerError
VENDOR_ERROR_UNMAPPED: MinerError
SEVERITY_UNSPECIFIED: Severity
SEVERITY_CRITICAL: Severity
SEVERITY_MAJOR: Severity
SEVERITY_MINOR: Severity
SEVERITY_INFO: Severity
COMPONENT_TYPE_UNSPECIFIED: ComponentType
COMPONENT_TYPE_PSU: ComponentType
COMPONENT_TYPE_HASH_BOARD: ComponentType
COMPONENT_TYPE_FAN: ComponentType
COMPONENT_TYPE_CONTROL_BOARD: ComponentType
COMPONENT_TYPE_EEPROM: ComponentType
COMPONENT_TYPE_IO_MODULE: ComponentType

class Capabilities(_message.Message):
    __slots__ = ("flags",)
    class FlagsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: bool
        def __init__(self, key: _Optional[str] = ..., value: bool = ...) -> None: ...
    FLAGS_FIELD_NUMBER: _ClassVar[int]
    flags: _containers.ScalarMap[str, bool]
    def __init__(self, flags: _Optional[_Mapping[str, bool]] = ...) -> None: ...

class HandshakeResponse(_message.Message):
    __slots__ = ("driver_name", "api_version")
    DRIVER_NAME_FIELD_NUMBER: _ClassVar[int]
    API_VERSION_FIELD_NUMBER: _ClassVar[int]
    driver_name: str
    api_version: str
    def __init__(self, driver_name: _Optional[str] = ..., api_version: _Optional[str] = ...) -> None: ...

class DescribeDriverResponse(_message.Message):
    __slots__ = ("driver_name", "api_version", "caps")
    DRIVER_NAME_FIELD_NUMBER: _ClassVar[int]
    API_VERSION_FIELD_NUMBER: _ClassVar[int]
    CAPS_FIELD_NUMBER: _ClassVar[int]
    driver_name: str
    api_version: str
    caps: Capabilities
    def __init__(self, driver_name: _Optional[str] = ..., api_version: _Optional[str] = ..., caps: _Optional[_Union[Capabilities, _Mapping]] = ...) -> None: ...

class NewDeviceRequest(_message.Message):
    __slots__ = ("device_id", "info", "secret")
    DEVICE_ID_FIELD_NUMBER: _ClassVar[int]
    INFO_FIELD_NUMBER: _ClassVar[int]
    SECRET_FIELD_NUMBER: _ClassVar[int]
    device_id: str
    info: DeviceInfo
    secret: SecretBundle
    def __init__(self, device_id: _Optional[str] = ..., info: _Optional[_Union[DeviceInfo, _Mapping]] = ..., secret: _Optional[_Union[SecretBundle, _Mapping]] = ...) -> None: ...

class NewDeviceResponse(_message.Message):
    __slots__ = ("device_id",)
    DEVICE_ID_FIELD_NUMBER: _ClassVar[int]
    device_id: str
    def __init__(self, device_id: _Optional[str] = ...) -> None: ...

class DeviceRef(_message.Message):
    __slots__ = ("device_id",)
    DEVICE_ID_FIELD_NUMBER: _ClassVar[int]
    device_id: str
    def __init__(self, device_id: _Optional[str] = ...) -> None: ...

class MetricValue(_message.Message):
    __slots__ = ("value", "kind", "metadata")
    VALUE_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    value: float
    kind: MetricKind
    metadata: MetricValueMetaData
    def __init__(self, value: _Optional[float] = ..., kind: _Optional[_Union[MetricKind, str]] = ..., metadata: _Optional[_Union[MetricValueMetaData, _Mapping]] = ...) -> None: ...

class MetricValueMetaData(_message.Message):
    __slots__ = ("window", "min", "max", "avg", "std_dev", "timestamp")
    WINDOW_FIELD_NUMBER: _ClassVar[int]
    MIN_FIELD_NUMBER: _ClassVar[int]
    MAX_FIELD_NUMBER: _ClassVar[int]
    AVG_FIELD_NUMBER: _ClassVar[int]
    STD_DEV_FIELD_NUMBER: _ClassVar[int]
    TIMESTAMP_FIELD_NUMBER: _ClassVar[int]
    window: _duration_pb2.Duration
    min: float
    max: float
    avg: float
    std_dev: float
    timestamp: _timestamp_pb2.Timestamp
    def __init__(self, window: _Optional[_Union[_duration_pb2.Duration, _Mapping]] = ..., min: _Optional[float] = ..., max: _Optional[float] = ..., avg: _Optional[float] = ..., std_dev: _Optional[float] = ..., timestamp: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class ComponentInfo(_message.Message):
    __slots__ = ("index", "name", "status", "status_reason", "timestamp")
    INDEX_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    STATUS_REASON_FIELD_NUMBER: _ClassVar[int]
    TIMESTAMP_FIELD_NUMBER: _ClassVar[int]
    index: int
    name: str
    status: ComponentStatus
    status_reason: str
    timestamp: _timestamp_pb2.Timestamp
    def __init__(self, index: _Optional[int] = ..., name: _Optional[str] = ..., status: _Optional[_Union[ComponentStatus, str]] = ..., status_reason: _Optional[str] = ..., timestamp: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class HashBoardMetrics(_message.Message):
    __slots__ = ("component_info", "serial_number", "hash_rate_hs", "temp_c", "voltage_v", "current_a", "inlet_temp_c", "outlet_temp_c", "ambient_temp_c", "chip_count", "chip_frequency_mhz", "asics", "fan_metrics")
    COMPONENT_INFO_FIELD_NUMBER: _ClassVar[int]
    SERIAL_NUMBER_FIELD_NUMBER: _ClassVar[int]
    HASH_RATE_HS_FIELD_NUMBER: _ClassVar[int]
    TEMP_C_FIELD_NUMBER: _ClassVar[int]
    VOLTAGE_V_FIELD_NUMBER: _ClassVar[int]
    CURRENT_A_FIELD_NUMBER: _ClassVar[int]
    INLET_TEMP_C_FIELD_NUMBER: _ClassVar[int]
    OUTLET_TEMP_C_FIELD_NUMBER: _ClassVar[int]
    AMBIENT_TEMP_C_FIELD_NUMBER: _ClassVar[int]
    CHIP_COUNT_FIELD_NUMBER: _ClassVar[int]
    CHIP_FREQUENCY_MHZ_FIELD_NUMBER: _ClassVar[int]
    ASICS_FIELD_NUMBER: _ClassVar[int]
    FAN_METRICS_FIELD_NUMBER: _ClassVar[int]
    component_info: ComponentInfo
    serial_number: str
    hash_rate_hs: MetricValue
    temp_c: MetricValue
    voltage_v: MetricValue
    current_a: MetricValue
    inlet_temp_c: MetricValue
    outlet_temp_c: MetricValue
    ambient_temp_c: MetricValue
    chip_count: int
    chip_frequency_mhz: MetricValue
    asics: _containers.RepeatedCompositeFieldContainer[ASICMetrics]
    fan_metrics: _containers.RepeatedCompositeFieldContainer[FanMetrics]
    def __init__(self, component_info: _Optional[_Union[ComponentInfo, _Mapping]] = ..., serial_number: _Optional[str] = ..., hash_rate_hs: _Optional[_Union[MetricValue, _Mapping]] = ..., temp_c: _Optional[_Union[MetricValue, _Mapping]] = ..., voltage_v: _Optional[_Union[MetricValue, _Mapping]] = ..., current_a: _Optional[_Union[MetricValue, _Mapping]] = ..., inlet_temp_c: _Optional[_Union[MetricValue, _Mapping]] = ..., outlet_temp_c: _Optional[_Union[MetricValue, _Mapping]] = ..., ambient_temp_c: _Optional[_Union[MetricValue, _Mapping]] = ..., chip_count: _Optional[int] = ..., chip_frequency_mhz: _Optional[_Union[MetricValue, _Mapping]] = ..., asics: _Optional[_Iterable[_Union[ASICMetrics, _Mapping]]] = ..., fan_metrics: _Optional[_Iterable[_Union[FanMetrics, _Mapping]]] = ...) -> None: ...

class ASICMetrics(_message.Message):
    __slots__ = ("component_info", "temp_c", "frequency_mhz", "voltage_v", "hashrate_hs")
    COMPONENT_INFO_FIELD_NUMBER: _ClassVar[int]
    TEMP_C_FIELD_NUMBER: _ClassVar[int]
    FREQUENCY_MHZ_FIELD_NUMBER: _ClassVar[int]
    VOLTAGE_V_FIELD_NUMBER: _ClassVar[int]
    HASHRATE_HS_FIELD_NUMBER: _ClassVar[int]
    component_info: ComponentInfo
    temp_c: MetricValue
    frequency_mhz: MetricValue
    voltage_v: MetricValue
    hashrate_hs: MetricValue
    def __init__(self, component_info: _Optional[_Union[ComponentInfo, _Mapping]] = ..., temp_c: _Optional[_Union[MetricValue, _Mapping]] = ..., frequency_mhz: _Optional[_Union[MetricValue, _Mapping]] = ..., voltage_v: _Optional[_Union[MetricValue, _Mapping]] = ..., hashrate_hs: _Optional[_Union[MetricValue, _Mapping]] = ...) -> None: ...

class PSUMetrics(_message.Message):
    __slots__ = ("component_info", "output_power_w", "output_voltage_v", "output_current_a", "input_power_w", "input_voltage_v", "input_current_a", "hotspot_temp_c", "efficiency_percent", "fan_metrics")
    COMPONENT_INFO_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_POWER_W_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_VOLTAGE_V_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_CURRENT_A_FIELD_NUMBER: _ClassVar[int]
    INPUT_POWER_W_FIELD_NUMBER: _ClassVar[int]
    INPUT_VOLTAGE_V_FIELD_NUMBER: _ClassVar[int]
    INPUT_CURRENT_A_FIELD_NUMBER: _ClassVar[int]
    HOTSPOT_TEMP_C_FIELD_NUMBER: _ClassVar[int]
    EFFICIENCY_PERCENT_FIELD_NUMBER: _ClassVar[int]
    FAN_METRICS_FIELD_NUMBER: _ClassVar[int]
    component_info: ComponentInfo
    output_power_w: MetricValue
    output_voltage_v: MetricValue
    output_current_a: MetricValue
    input_power_w: MetricValue
    input_voltage_v: MetricValue
    input_current_a: MetricValue
    hotspot_temp_c: MetricValue
    efficiency_percent: MetricValue
    fan_metrics: _containers.RepeatedCompositeFieldContainer[FanMetrics]
    def __init__(self, component_info: _Optional[_Union[ComponentInfo, _Mapping]] = ..., output_power_w: _Optional[_Union[MetricValue, _Mapping]] = ..., output_voltage_v: _Optional[_Union[MetricValue, _Mapping]] = ..., output_current_a: _Optional[_Union[MetricValue, _Mapping]] = ..., input_power_w: _Optional[_Union[MetricValue, _Mapping]] = ..., input_voltage_v: _Optional[_Union[MetricValue, _Mapping]] = ..., input_current_a: _Optional[_Union[MetricValue, _Mapping]] = ..., hotspot_temp_c: _Optional[_Union[MetricValue, _Mapping]] = ..., efficiency_percent: _Optional[_Union[MetricValue, _Mapping]] = ..., fan_metrics: _Optional[_Iterable[_Union[FanMetrics, _Mapping]]] = ...) -> None: ...

class FanMetrics(_message.Message):
    __slots__ = ("component_info", "rpm", "temp_c", "percent")
    COMPONENT_INFO_FIELD_NUMBER: _ClassVar[int]
    RPM_FIELD_NUMBER: _ClassVar[int]
    TEMP_C_FIELD_NUMBER: _ClassVar[int]
    PERCENT_FIELD_NUMBER: _ClassVar[int]
    component_info: ComponentInfo
    rpm: MetricValue
    temp_c: MetricValue
    percent: MetricValue
    def __init__(self, component_info: _Optional[_Union[ComponentInfo, _Mapping]] = ..., rpm: _Optional[_Union[MetricValue, _Mapping]] = ..., temp_c: _Optional[_Union[MetricValue, _Mapping]] = ..., percent: _Optional[_Union[MetricValue, _Mapping]] = ...) -> None: ...

class ControlBoardMetrics(_message.Message):
    __slots__ = ("component_info",)
    COMPONENT_INFO_FIELD_NUMBER: _ClassVar[int]
    component_info: ComponentInfo
    def __init__(self, component_info: _Optional[_Union[ComponentInfo, _Mapping]] = ...) -> None: ...

class SensorMetrics(_message.Message):
    __slots__ = ("component_info", "type", "unit", "value")
    COMPONENT_INFO_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    UNIT_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    component_info: ComponentInfo
    type: str
    unit: str
    value: MetricValue
    def __init__(self, component_info: _Optional[_Union[ComponentInfo, _Mapping]] = ..., type: _Optional[str] = ..., unit: _Optional[str] = ..., value: _Optional[_Union[MetricValue, _Mapping]] = ...) -> None: ...

class DeviceMetrics(_message.Message):
    __slots__ = ("device_id", "timestamp", "health", "health_reason", "hashrate_hs", "temp_c", "fan_rpm", "power_w", "efficiency_jh", "hash_boards", "psu_metrics", "control_board_metrics", "fan_metrics", "sensor_metrics", "firmware_version")
    DEVICE_ID_FIELD_NUMBER: _ClassVar[int]
    TIMESTAMP_FIELD_NUMBER: _ClassVar[int]
    HEALTH_FIELD_NUMBER: _ClassVar[int]
    HEALTH_REASON_FIELD_NUMBER: _ClassVar[int]
    HASHRATE_HS_FIELD_NUMBER: _ClassVar[int]
    TEMP_C_FIELD_NUMBER: _ClassVar[int]
    FAN_RPM_FIELD_NUMBER: _ClassVar[int]
    POWER_W_FIELD_NUMBER: _ClassVar[int]
    EFFICIENCY_JH_FIELD_NUMBER: _ClassVar[int]
    HASH_BOARDS_FIELD_NUMBER: _ClassVar[int]
    PSU_METRICS_FIELD_NUMBER: _ClassVar[int]
    CONTROL_BOARD_METRICS_FIELD_NUMBER: _ClassVar[int]
    FAN_METRICS_FIELD_NUMBER: _ClassVar[int]
    SENSOR_METRICS_FIELD_NUMBER: _ClassVar[int]
    FIRMWARE_VERSION_FIELD_NUMBER: _ClassVar[int]
    device_id: str
    timestamp: _timestamp_pb2.Timestamp
    health: HealthStatus
    health_reason: str
    hashrate_hs: MetricValue
    temp_c: MetricValue
    fan_rpm: MetricValue
    power_w: MetricValue
    efficiency_jh: MetricValue
    hash_boards: _containers.RepeatedCompositeFieldContainer[HashBoardMetrics]
    psu_metrics: _containers.RepeatedCompositeFieldContainer[PSUMetrics]
    control_board_metrics: _containers.RepeatedCompositeFieldContainer[ControlBoardMetrics]
    fan_metrics: _containers.RepeatedCompositeFieldContainer[FanMetrics]
    sensor_metrics: _containers.RepeatedCompositeFieldContainer[SensorMetrics]
    firmware_version: str
    def __init__(self, device_id: _Optional[str] = ..., timestamp: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ..., health: _Optional[_Union[HealthStatus, str]] = ..., health_reason: _Optional[str] = ..., hashrate_hs: _Optional[_Union[MetricValue, _Mapping]] = ..., temp_c: _Optional[_Union[MetricValue, _Mapping]] = ..., fan_rpm: _Optional[_Union[MetricValue, _Mapping]] = ..., power_w: _Optional[_Union[MetricValue, _Mapping]] = ..., efficiency_jh: _Optional[_Union[MetricValue, _Mapping]] = ..., hash_boards: _Optional[_Iterable[_Union[HashBoardMetrics, _Mapping]]] = ..., psu_metrics: _Optional[_Iterable[_Union[PSUMetrics, _Mapping]]] = ..., control_board_metrics: _Optional[_Iterable[_Union[ControlBoardMetrics, _Mapping]]] = ..., fan_metrics: _Optional[_Iterable[_Union[FanMetrics, _Mapping]]] = ..., sensor_metrics: _Optional[_Iterable[_Union[SensorMetrics, _Mapping]]] = ..., firmware_version: _Optional[str] = ...) -> None: ...

class DescribeDeviceRequest(_message.Message):
    __slots__ = ("device_id",)
    DEVICE_ID_FIELD_NUMBER: _ClassVar[int]
    device_id: str
    def __init__(self, device_id: _Optional[str] = ...) -> None: ...

class DescribeDeviceResponse(_message.Message):
    __slots__ = ("device", "caps")
    DEVICE_FIELD_NUMBER: _ClassVar[int]
    CAPS_FIELD_NUMBER: _ClassVar[int]
    device: DeviceInfo
    caps: Capabilities
    def __init__(self, device: _Optional[_Union[DeviceInfo, _Mapping]] = ..., caps: _Optional[_Union[Capabilities, _Mapping]] = ...) -> None: ...

class StatusBatchResponse(_message.Message):
    __slots__ = ("items",)
    ITEMS_FIELD_NUMBER: _ClassVar[int]
    items: _containers.RepeatedCompositeFieldContainer[DeviceMetrics]
    def __init__(self, items: _Optional[_Iterable[_Union[DeviceMetrics, _Mapping]]] = ...) -> None: ...

class BatchStatusRequest(_message.Message):
    __slots__ = ("refs",)
    REFS_FIELD_NUMBER: _ClassVar[int]
    refs: _containers.RepeatedCompositeFieldContainer[DeviceRef]
    def __init__(self, refs: _Optional[_Iterable[_Union[DeviceRef, _Mapping]]] = ...) -> None: ...

class SubscribeRequest(_message.Message):
    __slots__ = ("device_ids", "batch_size", "interval_seconds")
    DEVICE_IDS_FIELD_NUMBER: _ClassVar[int]
    BATCH_SIZE_FIELD_NUMBER: _ClassVar[int]
    INTERVAL_SECONDS_FIELD_NUMBER: _ClassVar[int]
    device_ids: _containers.RepeatedScalarFieldContainer[str]
    batch_size: int
    interval_seconds: int
    def __init__(self, device_ids: _Optional[_Iterable[str]] = ..., batch_size: _Optional[int] = ..., interval_seconds: _Optional[int] = ...) -> None: ...

class APIKey(_message.Message):
    __slots__ = ("key",)
    KEY_FIELD_NUMBER: _ClassVar[int]
    key: str
    def __init__(self, key: _Optional[str] = ...) -> None: ...

class UsernamePassword(_message.Message):
    __slots__ = ("username", "password")
    USERNAME_FIELD_NUMBER: _ClassVar[int]
    PASSWORD_FIELD_NUMBER: _ClassVar[int]
    username: str
    password: str
    def __init__(self, username: _Optional[str] = ..., password: _Optional[str] = ...) -> None: ...

class BearerToken(_message.Message):
    __slots__ = ("token",)
    TOKEN_FIELD_NUMBER: _ClassVar[int]
    token: str
    def __init__(self, token: _Optional[str] = ...) -> None: ...

class TlsClientCert(_message.Message):
    __slots__ = ("client_cert_pem", "key_pem", "ca_cert_pem")
    CLIENT_CERT_PEM_FIELD_NUMBER: _ClassVar[int]
    KEY_PEM_FIELD_NUMBER: _ClassVar[int]
    CA_CERT_PEM_FIELD_NUMBER: _ClassVar[int]
    client_cert_pem: bytes
    key_pem: bytes
    ca_cert_pem: bytes
    def __init__(self, client_cert_pem: _Optional[bytes] = ..., key_pem: _Optional[bytes] = ..., ca_cert_pem: _Optional[bytes] = ...) -> None: ...

class SecretBundle(_message.Message):
    __slots__ = ("version", "api_key", "user_pass", "bearer_token", "tls_client_cert", "ttl")
    VERSION_FIELD_NUMBER: _ClassVar[int]
    API_KEY_FIELD_NUMBER: _ClassVar[int]
    USER_PASS_FIELD_NUMBER: _ClassVar[int]
    BEARER_TOKEN_FIELD_NUMBER: _ClassVar[int]
    TLS_CLIENT_CERT_FIELD_NUMBER: _ClassVar[int]
    TTL_FIELD_NUMBER: _ClassVar[int]
    version: str
    api_key: APIKey
    user_pass: UsernamePassword
    bearer_token: BearerToken
    tls_client_cert: TlsClientCert
    ttl: _duration_pb2.Duration
    def __init__(self, version: _Optional[str] = ..., api_key: _Optional[_Union[APIKey, _Mapping]] = ..., user_pass: _Optional[_Union[UsernamePassword, _Mapping]] = ..., bearer_token: _Optional[_Union[BearerToken, _Mapping]] = ..., tls_client_cert: _Optional[_Union[TlsClientCert, _Mapping]] = ..., ttl: _Optional[_Union[_duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class SetCoolingModeRequest(_message.Message):
    __slots__ = ("ref", "mode")
    REF_FIELD_NUMBER: _ClassVar[int]
    MODE_FIELD_NUMBER: _ClassVar[int]
    ref: DeviceRef
    mode: CoolingMode
    def __init__(self, ref: _Optional[_Union[DeviceRef, _Mapping]] = ..., mode: _Optional[_Union[CoolingMode, str]] = ...) -> None: ...

class GetCoolingModeResponse(_message.Message):
    __slots__ = ("mode",)
    MODE_FIELD_NUMBER: _ClassVar[int]
    mode: CoolingMode
    def __init__(self, mode: _Optional[_Union[CoolingMode, str]] = ...) -> None: ...

class CurtailRequest(_message.Message):
    __slots__ = ("ref", "level")
    REF_FIELD_NUMBER: _ClassVar[int]
    LEVEL_FIELD_NUMBER: _ClassVar[int]
    ref: DeviceRef
    level: CurtailLevel
    def __init__(self, ref: _Optional[_Union[DeviceRef, _Mapping]] = ..., level: _Optional[_Union[CurtailLevel, str]] = ...) -> None: ...

class UncurtailRequest(_message.Message):
    __slots__ = ("ref",)
    REF_FIELD_NUMBER: _ClassVar[int]
    ref: DeviceRef
    def __init__(self, ref: _Optional[_Union[DeviceRef, _Mapping]] = ...) -> None: ...

class SetPowerTargetRequest(_message.Message):
    __slots__ = ("ref", "performance_mode")
    REF_FIELD_NUMBER: _ClassVar[int]
    PERFORMANCE_MODE_FIELD_NUMBER: _ClassVar[int]
    ref: DeviceRef
    performance_mode: PerformanceMode
    def __init__(self, ref: _Optional[_Union[DeviceRef, _Mapping]] = ..., performance_mode: _Optional[_Union[PerformanceMode, str]] = ...) -> None: ...

class MiningPool(_message.Message):
    __slots__ = ("priority", "url", "worker_name")
    PRIORITY_FIELD_NUMBER: _ClassVar[int]
    URL_FIELD_NUMBER: _ClassVar[int]
    WORKER_NAME_FIELD_NUMBER: _ClassVar[int]
    priority: int
    url: str
    worker_name: str
    def __init__(self, priority: _Optional[int] = ..., url: _Optional[str] = ..., worker_name: _Optional[str] = ...) -> None: ...

class UpdateMiningPoolsRequest(_message.Message):
    __slots__ = ("ref", "pools")
    REF_FIELD_NUMBER: _ClassVar[int]
    POOLS_FIELD_NUMBER: _ClassVar[int]
    ref: DeviceRef
    pools: _containers.RepeatedCompositeFieldContainer[MiningPool]
    def __init__(self, ref: _Optional[_Union[DeviceRef, _Mapping]] = ..., pools: _Optional[_Iterable[_Union[MiningPool, _Mapping]]] = ...) -> None: ...

class ConfiguredPool(_message.Message):
    __slots__ = ("priority", "url", "username")
    PRIORITY_FIELD_NUMBER: _ClassVar[int]
    URL_FIELD_NUMBER: _ClassVar[int]
    USERNAME_FIELD_NUMBER: _ClassVar[int]
    priority: int
    url: str
    username: str
    def __init__(self, priority: _Optional[int] = ..., url: _Optional[str] = ..., username: _Optional[str] = ...) -> None: ...

class GetMiningPoolsRequest(_message.Message):
    __slots__ = ("ref",)
    REF_FIELD_NUMBER: _ClassVar[int]
    ref: DeviceRef
    def __init__(self, ref: _Optional[_Union[DeviceRef, _Mapping]] = ...) -> None: ...

class GetMiningPoolsResponse(_message.Message):
    __slots__ = ("pools",)
    POOLS_FIELD_NUMBER: _ClassVar[int]
    pools: _containers.RepeatedCompositeFieldContainer[ConfiguredPool]
    def __init__(self, pools: _Optional[_Iterable[_Union[ConfiguredPool, _Mapping]]] = ...) -> None: ...

class DownloadLogsRequest(_message.Message):
    __slots__ = ("ref", "since", "batch_log_uuid")
    REF_FIELD_NUMBER: _ClassVar[int]
    SINCE_FIELD_NUMBER: _ClassVar[int]
    BATCH_LOG_UUID_FIELD_NUMBER: _ClassVar[int]
    ref: DeviceRef
    since: _timestamp_pb2.Timestamp
    batch_log_uuid: str
    def __init__(self, ref: _Optional[_Union[DeviceRef, _Mapping]] = ..., since: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ..., batch_log_uuid: _Optional[str] = ...) -> None: ...

class DownloadLogsResponse(_message.Message):
    __slots__ = ("log_data", "more_data")
    LOG_DATA_FIELD_NUMBER: _ClassVar[int]
    MORE_DATA_FIELD_NUMBER: _ClassVar[int]
    log_data: str
    more_data: bool
    def __init__(self, log_data: _Optional[str] = ..., more_data: bool = ...) -> None: ...

class DeviceInfo(_message.Message):
    __slots__ = ("host", "port", "url_scheme", "serial_number", "model", "manufacturer", "mac_address", "firmware_version")
    HOST_FIELD_NUMBER: _ClassVar[int]
    PORT_FIELD_NUMBER: _ClassVar[int]
    URL_SCHEME_FIELD_NUMBER: _ClassVar[int]
    SERIAL_NUMBER_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    MANUFACTURER_FIELD_NUMBER: _ClassVar[int]
    MAC_ADDRESS_FIELD_NUMBER: _ClassVar[int]
    FIRMWARE_VERSION_FIELD_NUMBER: _ClassVar[int]
    host: str
    port: int
    url_scheme: str
    serial_number: str
    model: str
    manufacturer: str
    mac_address: str
    firmware_version: str
    def __init__(self, host: _Optional[str] = ..., port: _Optional[int] = ..., url_scheme: _Optional[str] = ..., serial_number: _Optional[str] = ..., model: _Optional[str] = ..., manufacturer: _Optional[str] = ..., mac_address: _Optional[str] = ..., firmware_version: _Optional[str] = ...) -> None: ...

class DiscoverDeviceRequest(_message.Message):
    __slots__ = ("ip_address", "port")
    IP_ADDRESS_FIELD_NUMBER: _ClassVar[int]
    PORT_FIELD_NUMBER: _ClassVar[int]
    ip_address: str
    port: str
    def __init__(self, ip_address: _Optional[str] = ..., port: _Optional[str] = ...) -> None: ...

class DiscoverDeviceResponse(_message.Message):
    __slots__ = ("device",)
    DEVICE_FIELD_NUMBER: _ClassVar[int]
    device: DeviceInfo
    def __init__(self, device: _Optional[_Union[DeviceInfo, _Mapping]] = ...) -> None: ...

class PairDeviceRequest(_message.Message):
    __slots__ = ("device", "access")
    DEVICE_FIELD_NUMBER: _ClassVar[int]
    ACCESS_FIELD_NUMBER: _ClassVar[int]
    device: DeviceInfo
    access: SecretBundle
    def __init__(self, device: _Optional[_Union[DeviceInfo, _Mapping]] = ..., access: _Optional[_Union[SecretBundle, _Mapping]] = ...) -> None: ...

class PairDeviceResponse(_message.Message):
    __slots__ = ("device",)
    DEVICE_FIELD_NUMBER: _ClassVar[int]
    device: DeviceInfo
    def __init__(self, device: _Optional[_Union[DeviceInfo, _Mapping]] = ...) -> None: ...

class GetDefaultCredentialsRequest(_message.Message):
    __slots__ = ("manufacturer", "firmware_version")
    MANUFACTURER_FIELD_NUMBER: _ClassVar[int]
    FIRMWARE_VERSION_FIELD_NUMBER: _ClassVar[int]
    manufacturer: str
    firmware_version: str
    def __init__(self, manufacturer: _Optional[str] = ..., firmware_version: _Optional[str] = ...) -> None: ...

class GetDefaultCredentialsResponse(_message.Message):
    __slots__ = ("credentials",)
    CREDENTIALS_FIELD_NUMBER: _ClassVar[int]
    credentials: _containers.RepeatedCompositeFieldContainer[UsernamePassword]
    def __init__(self, credentials: _Optional[_Iterable[_Union[UsernamePassword, _Mapping]]] = ...) -> None: ...

class GetCapabilitiesForModelRequest(_message.Message):
    __slots__ = ("model", "manufacturer")
    MODEL_FIELD_NUMBER: _ClassVar[int]
    MANUFACTURER_FIELD_NUMBER: _ClassVar[int]
    model: str
    manufacturer: str
    def __init__(self, model: _Optional[str] = ..., manufacturer: _Optional[str] = ...) -> None: ...

class GetCapabilitiesForModelResponse(_message.Message):
    __slots__ = ("caps",)
    CAPS_FIELD_NUMBER: _ClassVar[int]
    caps: Capabilities
    def __init__(self, caps: _Optional[_Union[Capabilities, _Mapping]] = ...) -> None: ...

class GetDiscoveryPortsResponse(_message.Message):
    __slots__ = ("ports",)
    PORTS_FIELD_NUMBER: _ClassVar[int]
    ports: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, ports: _Optional[_Iterable[str]] = ...) -> None: ...

class GetDeviceWebViewURLResponse(_message.Message):
    __slots__ = ("url",)
    URL_FIELD_NUMBER: _ClassVar[int]
    url: str
    def __init__(self, url: _Optional[str] = ...) -> None: ...

class GetDeviceWebViewURLRequest(_message.Message):
    __slots__ = ("ref",)
    REF_FIELD_NUMBER: _ClassVar[int]
    ref: DeviceRef
    def __init__(self, ref: _Optional[_Union[DeviceRef, _Mapping]] = ...) -> None: ...

class GetTimeSeriesDataRequest(_message.Message):
    __slots__ = ("ref", "metric_names", "start_time", "end_time", "granularity", "max_points", "page_token")
    REF_FIELD_NUMBER: _ClassVar[int]
    METRIC_NAMES_FIELD_NUMBER: _ClassVar[int]
    START_TIME_FIELD_NUMBER: _ClassVar[int]
    END_TIME_FIELD_NUMBER: _ClassVar[int]
    GRANULARITY_FIELD_NUMBER: _ClassVar[int]
    MAX_POINTS_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    ref: DeviceRef
    metric_names: _containers.RepeatedScalarFieldContainer[str]
    start_time: _timestamp_pb2.Timestamp
    end_time: _timestamp_pb2.Timestamp
    granularity: _duration_pb2.Duration
    max_points: int
    page_token: str
    def __init__(self, ref: _Optional[_Union[DeviceRef, _Mapping]] = ..., metric_names: _Optional[_Iterable[str]] = ..., start_time: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ..., end_time: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ..., granularity: _Optional[_Union[_duration_pb2.Duration, _Mapping]] = ..., max_points: _Optional[int] = ..., page_token: _Optional[str] = ...) -> None: ...

class GetTimeSeriesDataResponse(_message.Message):
    __slots__ = ("series", "next_page_token")
    SERIES_FIELD_NUMBER: _ClassVar[int]
    NEXT_PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    series: _containers.RepeatedCompositeFieldContainer[DeviceMetrics]
    next_page_token: str
    def __init__(self, series: _Optional[_Iterable[_Union[DeviceMetrics, _Mapping]]] = ..., next_page_token: _Optional[str] = ...) -> None: ...

class UpdateMinerPasswordRequest(_message.Message):
    __slots__ = ("ref", "current_password", "new_password")
    REF_FIELD_NUMBER: _ClassVar[int]
    CURRENT_PASSWORD_FIELD_NUMBER: _ClassVar[int]
    NEW_PASSWORD_FIELD_NUMBER: _ClassVar[int]
    ref: DeviceRef
    current_password: str
    new_password: str
    def __init__(self, ref: _Optional[_Union[DeviceRef, _Mapping]] = ..., current_password: _Optional[str] = ..., new_password: _Optional[str] = ...) -> None: ...

class FirmwareFileInfo(_message.Message):
    __slots__ = ("file_path", "original_filename", "file_size")
    FILE_PATH_FIELD_NUMBER: _ClassVar[int]
    ORIGINAL_FILENAME_FIELD_NUMBER: _ClassVar[int]
    FILE_SIZE_FIELD_NUMBER: _ClassVar[int]
    file_path: str
    original_filename: str
    file_size: int
    def __init__(self, file_path: _Optional[str] = ..., original_filename: _Optional[str] = ..., file_size: _Optional[int] = ...) -> None: ...

class UpdateFirmwareRequest(_message.Message):
    __slots__ = ("ref", "firmware")
    REF_FIELD_NUMBER: _ClassVar[int]
    FIRMWARE_FIELD_NUMBER: _ClassVar[int]
    ref: DeviceRef
    firmware: FirmwareFileInfo
    def __init__(self, ref: _Optional[_Union[DeviceRef, _Mapping]] = ..., firmware: _Optional[_Union[FirmwareFileInfo, _Mapping]] = ...) -> None: ...

class GetFirmwareUpdateStatusResponse(_message.Message):
    __slots__ = ("state", "progress", "error")
    STATE_FIELD_NUMBER: _ClassVar[int]
    PROGRESS_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    state: str
    progress: int
    error: str
    def __init__(self, state: _Optional[str] = ..., progress: _Optional[int] = ..., error: _Optional[str] = ...) -> None: ...

class DeviceError(_message.Message):
    __slots__ = ("miner_error", "cause_summary", "recommended_action", "severity", "first_seen_at", "last_seen_at", "closed_at", "vendor_attributes", "device_id", "component_id", "impact", "summary", "component_type")
    class VendorAttributesEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    MINER_ERROR_FIELD_NUMBER: _ClassVar[int]
    CAUSE_SUMMARY_FIELD_NUMBER: _ClassVar[int]
    RECOMMENDED_ACTION_FIELD_NUMBER: _ClassVar[int]
    SEVERITY_FIELD_NUMBER: _ClassVar[int]
    FIRST_SEEN_AT_FIELD_NUMBER: _ClassVar[int]
    LAST_SEEN_AT_FIELD_NUMBER: _ClassVar[int]
    CLOSED_AT_FIELD_NUMBER: _ClassVar[int]
    VENDOR_ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
    DEVICE_ID_FIELD_NUMBER: _ClassVar[int]
    COMPONENT_ID_FIELD_NUMBER: _ClassVar[int]
    IMPACT_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_FIELD_NUMBER: _ClassVar[int]
    COMPONENT_TYPE_FIELD_NUMBER: _ClassVar[int]
    miner_error: MinerError
    cause_summary: str
    recommended_action: str
    severity: Severity
    first_seen_at: _timestamp_pb2.Timestamp
    last_seen_at: _timestamp_pb2.Timestamp
    closed_at: _timestamp_pb2.Timestamp
    vendor_attributes: _containers.ScalarMap[str, str]
    device_id: str
    component_id: str
    impact: str
    summary: str
    component_type: ComponentType
    def __init__(self, miner_error: _Optional[_Union[MinerError, str]] = ..., cause_summary: _Optional[str] = ..., recommended_action: _Optional[str] = ..., severity: _Optional[_Union[Severity, str]] = ..., first_seen_at: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ..., last_seen_at: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ..., closed_at: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ..., vendor_attributes: _Optional[_Mapping[str, str]] = ..., device_id: _Optional[str] = ..., component_id: _Optional[str] = ..., impact: _Optional[str] = ..., summary: _Optional[str] = ..., component_type: _Optional[_Union[ComponentType, str]] = ...) -> None: ...

class DeviceErrors(_message.Message):
    __slots__ = ("device_id", "errors")
    DEVICE_ID_FIELD_NUMBER: _ClassVar[int]
    ERRORS_FIELD_NUMBER: _ClassVar[int]
    device_id: str
    errors: _containers.RepeatedCompositeFieldContainer[DeviceError]
    def __init__(self, device_id: _Optional[str] = ..., errors: _Optional[_Iterable[_Union[DeviceError, _Mapping]]] = ...) -> None: ...
