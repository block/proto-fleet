"""Utility functions for Proto Fleet SDK.

This module provides helper functions for common operations like port parsing,
type conversions, validation, HTTP error mapping, and unit conversions.
"""

from proto_fleet_sdk.telemetry.converters import (
    hs_to_ths,
    jh_to_jth,
    jth_to_jh,
    ths_to_hs,
)
from proto_fleet_sdk.utils.capability_helpers import (
    has_capability,
    merge_capabilities,
)
from proto_fleet_sdk.utils.http_utils import map_http_error
from proto_fleet_sdk.utils.port_utils import parse_port
from proto_fleet_sdk.utils.type_converters import safe_index_conversion, safe_uint_to_int32
from proto_fleet_sdk.utils.validation import validate_capabilities, validate_device_metrics

__all__ = [
    "parse_port",
    "safe_uint_to_int32",
    "safe_index_conversion",
    "validate_device_metrics",
    "validate_capabilities",
    "has_capability",
    "merge_capabilities",
    "map_http_error",
    "ths_to_hs",
    "jth_to_jh",
    "hs_to_ths",
    "jh_to_jth",
]
