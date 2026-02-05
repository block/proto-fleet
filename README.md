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

## Production protoFleet single script install

### Latest version

```shell
bash <(curl -fsSL https://proto-fleet.s3.us-east-1.amazonaws.com/releases/fleet/latest/install.sh)
```

### Specific version

```shell
bash <(curl -fsSL https://proto-fleet.s3.us-east-1.amazonaws.com/releases/fleet/latest/install.sh) v0.1.0
```

### Environment variables

* With defaults
  * Database username [fleet_user]
* Secret (without defaults)
  * Database password
  * Auth client secret key (at least 32 characters)
  * Pairing secret key (32-48 characters)
* Secret + generable
  * Encryption service master key
    * 32 bytes chain encoded in Base64 - awaits 44 characters
    * generable via "<b>openssl rand -base64 32</b>"

### Unix

```shell
chmod +x run-fleet.sh
./run-fleet.sh
```

### Windows

Support for Windows is coming soon.

## related docs

https://connectrpc.com/docs/web/generating-code
https://connectrpc.com/docs/go/getting-started
https://connectrpc.com/docs/web/using-clients
