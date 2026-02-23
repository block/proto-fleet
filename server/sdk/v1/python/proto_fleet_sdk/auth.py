"""Authentication types for Proto Fleet SDK.

This module defines authentication credential types for accessing mining devices.
Credentials are provided as SecretBundle with different authentication kinds.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import timedelta

__all__ = [
    "APIKey",
    "UsernamePassword",
    "BearerToken",
    "TLSClientCert",
    "SecretBundle",
]


@dataclass(frozen=True, repr=False)
class APIKey:
    """API key authentication."""

    key: str

    def __str__(self) -> str:
        return "APIKey(key=***)"

    __repr__ = __str__


@dataclass(frozen=True, repr=False)
class UsernamePassword:
    """Username and password authentication."""

    username: str
    password: str

    def __str__(self) -> str:
        return f"UsernamePassword(username={self.username!r}, password=***)"

    __repr__ = __str__


@dataclass(frozen=True, repr=False)
class BearerToken:
    """Bearer token authentication."""

    token: str

    def __str__(self) -> str:
        return "BearerToken(token=***)"

    __repr__ = __str__


@dataclass(frozen=True, repr=False)
class TLSClientCert:
    """TLS client certificate authentication."""

    client_cert_pem: bytes
    key_pem: bytes
    ca_cert_pem: bytes

    def __str__(self) -> str:
        return (
            f"TLSClientCert(client_cert_pem=<{len(self.client_cert_pem)} bytes>, "
            f"key_pem=<{len(self.key_pem)} bytes>, "
            f"ca_cert_pem=<{len(self.ca_cert_pem)} bytes>)"
        )

    __repr__ = __str__


@dataclass(frozen=True)
class SecretBundle:
    """Bundle of authentication credentials with metadata.

    The kind field contains one of: APIKey, UsernamePassword, BearerToken, or TLSClientCert.
    """

    version: str
    kind: APIKey | UsernamePassword | BearerToken | TLSClientCert
    ttl: timedelta | None = None

    def __str__(self) -> str:
        kind_type = type(self.kind).__name__
        ttl_str = f", ttl={self.ttl.total_seconds()}s" if self.ttl else ""
        return f"SecretBundle(version={self.version!r}, kind={kind_type}{ttl_str})"
