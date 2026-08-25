#!/usr/bin/env python3

import json
import os
import string
import sys
from pathlib import Path


PASSWORD_FILES = {
    "PATRONI_ETCD_PASSWORD_YAML": "patroni-etcd-password",
    "PATRONI_REST_PASSWORD_YAML": "patroni-rest-password",
    "POSTGRES_SUPERUSER_PASSWORD_YAML": "postgres-superuser-password",
    "POSTGRES_REPLICATION_PASSWORD_YAML": "postgres-replication-password",
}


def main() -> None:
    if len(sys.argv) != 4:
        raise SystemExit(
            "usage: render-patroni-config TEMPLATE SECRETS_DIRECTORY OUTPUT"
        )

    template_path = Path(sys.argv[1])
    secrets_directory = Path(sys.argv[2])
    output_path = Path(sys.argv[3])
    substitutions = dict(os.environ)

    for variable, filename in PASSWORD_FILES.items():
        password = (secrets_directory / filename).read_text(encoding="utf-8")
        substitutions[variable] = json.dumps(password.rstrip("\n"))

    rendered = string.Template(template_path.read_text(encoding="utf-8")).substitute(
        substitutions
    )
    output_path.write_text(rendered, encoding="utf-8")


if __name__ == "__main__":
    main()
