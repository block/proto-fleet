#!/usr/bin/env python3
"""Start Patroni without logging its etcd authentication token."""

import logging

from patroni.__main__ import main


def redact_etcd_token(record):
    # Patroni 4.1.4 includes the rejected token as a logging argument. Remove it
    # before the record reaches either the text or JSON handler.
    if record.msg == "Invalid auth token: %s":
        record.msg = "Invalid auth token: [redacted]"
        record.args = ()
    return True


if __name__ == "__main__":
    logging.getLogger("patroni.dcs.etcd3").addFilter(redact_etcd_token)
    main()
