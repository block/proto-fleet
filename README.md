# Proto Web

Home for all proto mining frontend and backend web code.

## Activate Hermit environment

```
./bin/activate-hermit
```

## Protocol Buffer code gen

### setup

Run the blow command to install the proto

```
npm install -g @bufbuild/buf @bufbuild/protoc-gen-es
```

### develop

Make updates to protocol buffer files in `proto` then run the below to generate code in the `server` and `client` project.

```
just gen
```

commit generated changes.

## related docs

https://connectrpc.com/docs/web/generating-code
https://connectrpc.com/docs/go/getting-started
https://connectrpc.com/docs/web/using-clients
