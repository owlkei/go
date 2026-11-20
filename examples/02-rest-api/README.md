# 02 · REST API

A REST API for a small library's books. It adds what a real API needs on top of
[01 · Hello](../01-hello/README.md): a module, a service shared by the routes, input validation and
the errors a client gets back.

## What you learn

- **A module** (`books.go`) groups a controller with the provider it reads.
- **A provider** (`catalog.go`) is injected into the controller by its type: the controller declares a
  `Catalog` field and Owlkei Go fills it. State that every copy shares sits behind a pointer.
- **A pipe** (`book_input.go`) binds the JSON body and validates it before the handler runs. A
  refused body answers `422` and never reaches the handler.
- **Exceptions** become the response: `exception.NotFoundException` answers `404` with your message.
- **Status codes** follow the method: `POST` answers `201`, the others `200`.

## Run it

```sh
go run .
```

```sh
curl localhost:3000/books
curl localhost:3000/books/2
curl -X POST localhost:3000/books -H 'Content-Type: application/json' \
  -d '{"title":"Go in Action","author":"William Kennedy","year":2015}'
curl -X PUT localhost:3000/books/1 -H 'Content-Type: application/json' \
  -d '{"title":"The Go Programming Language","author":"Alan Donovan, Brian Kernighan","year":2015}'
curl -X DELETE localhost:3000/books/2
```

A refused body and a missing book:

```sh
curl -X POST localhost:3000/books -H 'Content-Type: application/json' -d '{"title":" ","year":3000}'
# 422 {"code":422,"error":"Unprocessable Entity","message":"title is required; author is required; year must be between 1450 and this year"}

curl localhost:3000/books/9
# 404 {"code":404,"error":"Not Found","message":"No book with id 9"}
```

`go test ./...` checks every route and both errors.

## Routes

| Method name | Route |
|---|---|
| `READ_books` | `GET /books` |
| `READ_books_BY_id` | `GET /books/{id}` |
| `CREATE_books` | `POST /books` |
| `UPDATE_books_BY_id` | `PUT /books/{id}` |
| `DELETE_books_BY_id` | `DELETE /books/{id}` |

## Read next

[03 · Pages](../03-pages/README.md): the same framework, answering HTML instead of JSON.
