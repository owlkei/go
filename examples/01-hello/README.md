# 01 · Hello

The smallest Owlkei Go app. One controller, two routes, and no routing code: the method names are the
routes.

## What you learn

- A controller embeds `owlkei.HTTP` and returns itself from `NewController`.
- `READ_hello` answers `GET /hello`. `READ_hello_BY_name` answers `GET /hello/{name}`.
- A handler returns a value: a string is sent as text, a map as JSON.
- An unknown path answers `404` with a JSON error, without any code of yours.

## Run it

```sh
go run .
```

```sh
curl localhost:3000/hello        # Hello from Owlkei Go
curl localhost:3000/hello/ada    # {"greeting":"Hello, ada"}
curl localhost:3000/goodbye      # {"code":404,"error":"Not Found","message":"Not Found"}
```

`go test ./...` checks the same three calls.

## Read next

[02 · REST API](../02-rest-api/README.md): modules, services and validation.
