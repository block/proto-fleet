# Proto Web

Home for all proto mining frontend and backend web code.

## Activate Hermit environment

```shell
./bin/activate-hermit
```

## Protocol Buffer code gen

### setup

Run the below commands to install the connectrpc dependencies

```bash
just init # runs commands to install the protoc deps
```

### develop

Make updates to protocol buffer files in `proto` then run the below to generate code in the `server` and `client` project.

```shell
just gen
```

commit generated changes.

## Start client and server

```shell
just dev
```

This will:

1. Run protoFleet client and server

2. Print the local web url (default: http://localhost:5173) in the console

3. Shutdown all processes if stopped

## related docs

https://connectrpc.com/docs/web/generating-code
https://connectrpc.com/docs/go/getting-started
https://connectrpc.com/docs/web/using-clients
