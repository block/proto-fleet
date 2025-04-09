This folder contains protocol buffers for rpc between miner-web and fleet.

## setup

Run the blow command to install the proto

```
npm install -g @bufbuild/buf @bufbuild/protoc-gen-es
```

## develop

Make updates to protocol buffer files in `fleet-protobuf` then run the below to generate code in the `fleet` and `miner-web` project.

```
just gen
```

commit generated changes.

## related docs

https://connectrpc.com/docs/web/generating-code
https://connectrpc.com/docs/go/getting-started
https://connectrpc.com/docs/web/using-clients
