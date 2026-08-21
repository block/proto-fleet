#!/usr/bin/env python3
"""Tiny fault-injection TCP proxy for local miner refresh testing.

Example:
  docker run --rm --name miner-fault-proxy \
    --network protofleet-infra_fleet-network --ip 192.168.2.50 \
    -p 19000:9000 \
    -v "$PWD/scripts/dev/miner_fault_proxy.py:/proxy.py:ro" \
    python:3.12-alpine \
    python /proxy.py \
      --fault-port 4028 \
      --forward 0.0.0.0:4028=192.168.2.19:4028 \
      --forward 0.0.0.0:80=192.168.2.19:80

Then point one discovered_device.ip_address at 192.168.2.50 and toggle:
  curl -X POST localhost:19000/mode/close
  curl -X POST localhost:19000/mode/pass
"""

from __future__ import annotations

import argparse
import asyncio
import contextlib
import json
import signal
from dataclasses import dataclass


MODES = {"pass", "close", "http-500", "garbage", "timeout"}


@dataclass
class ProxyState:
    mode: str


@dataclass(frozen=True)
class Forward:
    listen: tuple[str, int]
    target: tuple[str, int]


def parse_host_port(value: str) -> tuple[str, int]:
    host, sep, port = value.rpartition(":")
    if not sep or not host or not port:
        raise argparse.ArgumentTypeError("expected HOST:PORT")
    return host, int(port)


def parse_forward(value: str) -> Forward:
    listen, sep, target = value.partition("=")
    if not sep:
        raise argparse.ArgumentTypeError("expected LISTEN_HOST:PORT=TARGET_HOST:PORT")
    return Forward(listen=parse_host_port(listen), target=parse_host_port(target))


async def pipe(reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
    try:
        while data := await reader.read(65536):
            writer.write(data)
            await writer.drain()
    except (ConnectionError, asyncio.CancelledError):
        pass
    finally:
        with contextlib.suppress(Exception):
            writer.close()
            await writer.wait_closed()


async def handle_proxy(
    client_reader: asyncio.StreamReader,
    client_writer: asyncio.StreamWriter,
    target: tuple[str, int],
    state: ProxyState,
    timeout_seconds: float,
    fault_label: str | None,
) -> None:
    peer = client_writer.get_extra_info("peername")
    mode = state.mode if fault_label is not None else "pass"
    print(
        f"miner connection from {peer}; target={target[0]}:{target[1]}; mode={mode}; fault={fault_label or 'off'}",
        flush=True,
    )

    if mode == "close":
        client_writer.close()
        await client_writer.wait_closed()
        return

    if mode == "http-500":
        client_writer.write(
            b"HTTP/1.1 500 Internal Server Error\r\n"
            b"content-type: text/plain\r\n"
            b"content-length: 27\r\n"
            b"connection: close\r\n"
            b"\r\n"
            b"injected miner proxy error\n"
        )
        await client_writer.drain()
        client_writer.close()
        await client_writer.wait_closed()
        return

    if mode == "garbage":
        client_writer.write(b"not-json-not-http injected miner proxy error\n")
        await client_writer.drain()
        client_writer.close()
        await client_writer.wait_closed()
        return

    if mode == "timeout":
        await asyncio.sleep(timeout_seconds)
        client_writer.close()
        await client_writer.wait_closed()
        return

    try:
        target_reader, target_writer = await asyncio.open_connection(*target)
    except Exception as exc:
        print(f"target connect failed: {exc}", flush=True)
        client_writer.close()
        await client_writer.wait_closed()
        return

    tasks = [
        asyncio.create_task(pipe(client_reader, target_writer)),
        asyncio.create_task(pipe(target_reader, client_writer)),
    ]
    done, pending = await asyncio.wait(tasks, return_when=asyncio.FIRST_COMPLETED)
    for task in pending:
        task.cancel()
    for task in done:
        with contextlib.suppress(Exception):
            await task


async def read_http_request(reader: asyncio.StreamReader) -> tuple[str, str]:
    line = await reader.readline()
    if not line:
        return "", ""
    parts = line.decode("utf-8", errors="replace").strip().split()
    while True:
        header = await reader.readline()
        if header in {b"\r\n", b"\n", b""}:
            break
    if len(parts) < 2:
        return "", ""
    return parts[0].upper(), parts[1]


async def write_json(
    writer: asyncio.StreamWriter,
    status: str,
    payload: dict[str, object],
) -> None:
    body = json.dumps(payload, sort_keys=True).encode("utf-8") + b"\n"
    writer.write(
        f"HTTP/1.1 {status}\r\n"
        "content-type: application/json\r\n"
        f"content-length: {len(body)}\r\n"
        "connection: close\r\n"
        "\r\n".encode("utf-8")
        + body
    )
    await writer.drain()
    writer.close()
    await writer.wait_closed()


async def handle_control(
    reader: asyncio.StreamReader,
    writer: asyncio.StreamWriter,
    state: ProxyState,
    forwards: list[Forward],
    fault_ports: set[int],
) -> None:
    method, path = await read_http_request(reader)
    if method == "GET" and path == "/":
        await write_json(
            writer,
            "200 OK",
            {
                "fault_ports": sorted(fault_ports),
                "forwards": [
                    f"{f.listen[0]}:{f.listen[1]}={f.target[0]}:{f.target[1]}"
                    for f in forwards
                ],
                "mode": state.mode,
                "modes": sorted(MODES),
            },
        )
        return

    if method == "POST" and path.startswith("/mode/"):
        mode = path.removeprefix("/mode/")
        if mode not in MODES:
            await write_json(
                writer,
                "400 Bad Request",
                {"error": f"unknown mode {mode}", "modes": sorted(MODES)},
            )
            return
        state.mode = mode
        print(f"control changed mode={mode}", flush=True)
        await write_json(writer, "200 OK", {"mode": state.mode})
        return

    await write_json(
        writer,
        "404 Not Found",
        {"error": "use GET / or POST /mode/{pass|close|http-500|garbage|timeout}"},
    )


async def main() -> None:
    parser = argparse.ArgumentParser(
        description="Tiny TCP proxy with fault injection modes."
    )
    parser.add_argument(
        "--forward",
        action="append",
        type=parse_forward,
        help="Port mapping as LISTEN_HOST:PORT=TARGET_HOST:PORT. Repeat for multiple ports.",
    )
    parser.add_argument(
        "--listen", type=parse_host_port, help="Deprecated. Use --forward."
    )
    parser.add_argument(
        "--target", type=parse_host_port, help="Deprecated. Use --forward."
    )
    parser.add_argument("--control", default="0.0.0.0:9000", type=parse_host_port)
    parser.add_argument(
        "--fault-port",
        action="append",
        default=[],
        type=int,
        help="Listen port that gets injected failures. Repeat to fault multiple ports. Defaults to 4028.",
    )
    parser.add_argument("--mode", default="pass", choices=sorted(MODES))
    parser.add_argument("--timeout-seconds", default=30, type=float)
    args = parser.parse_args()

    forwards = args.forward or []
    if args.listen or args.target:
        if not args.listen or not args.target:
            parser.error("--listen and --target must be used together")
        forwards.append(Forward(listen=args.listen, target=args.target))
    if not forwards:
        parser.error("at least one --forward LISTEN=TARGET is required")

    state = ProxyState(mode=args.mode)
    fault_ports = set(args.fault_port or [4028])
    proxy_servers = []
    for forward in forwards:
        proxy_servers.append(
            await asyncio.start_server(
                lambda r, w, forward=forward: handle_proxy(
                    r,
                    w,
                    forward.target,
                    state,
                    args.timeout_seconds,
                    str(forward.listen[1])
                    if forward.listen[1] in fault_ports
                    else None,
                ),
                *forward.listen,
            )
        )

    control_server = await asyncio.start_server(
        lambda r, w: handle_control(r, w, state, forwards, fault_ports),
        *args.control,
    )

    for forward in forwards:
        print(
            f"proxy listening on {forward.listen[0]}:{forward.listen[1]} -> "
            f"{forward.target[0]}:{forward.target[1]}",
            flush=True,
        )
    print(
        f"fault_ports={sorted(fault_ports)}; control on {args.control[0]}:{args.control[1]}; mode={state.mode}",
        flush=True,
    )

    stop = asyncio.Event()
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(sig, stop.set)

    async with contextlib.AsyncExitStack() as stack:
        for server in proxy_servers:
            await stack.enter_async_context(server)
        await stack.enter_async_context(control_server)
        await stop.wait()


if __name__ == "__main__":
    asyncio.run(main())
