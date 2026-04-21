"""Helper functions for working with capability flags."""

from __future__ import annotations

__all__ = [
    "has_capability",
    "merge_capabilities",
]

# Capabilities are just dict[str, bool] — no need for a type alias import.
Capabilities = dict[str, bool]


def has_capability(caps: Capabilities, capability: str) -> bool:
    """Check if a specific capability is supported.

    Example:
        >>> caps = {"reboot": True, "status": True}
        >>> has_capability(caps, "reboot")
        True
        >>> has_capability(caps, "firmware_update")
        False
    """
    return caps.get(capability, False)


def merge_capabilities(*caps_list: Capabilities) -> Capabilities:
    """Merge multiple capability dictionaries, later ones override earlier.

    Example:
        >>> base = {"reboot": True, "status": True}
        >>> extra = {"firmware_update": True}
        >>> merge_capabilities(base, extra)
        {'reboot': True, 'status': True, 'firmware_update': True}
    """
    result: Capabilities = {}
    for caps in caps_list:
        result.update(caps)
    return result
