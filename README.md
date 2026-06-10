# OwlDB

OwlDB is a lightweight NoSQL document database implemented in Go. It provides an HTTP-based database server for storing, retrieving, updating, and deleting JSON documents with schema validation and token-based authentication.

The project focuses on backend systems design, document-oriented storage, JSON processing, request handling, and modular database architecture.

## Features

* HTTP-based NoSQL document database
* JSON document storage and retrieval
* Document creation, update, deletion, and patching
* JSON schema validation
* Token-based authentication
* Structured logging
* Ordered in-memory storage using a skip list
* Modular package organization for database, server, authentication, schema, and patch logic
* Test coverage for core components

## Tech Stack

* Go
* HTTP server programming
* JSON / JSON Schema
* Token-based authentication
* Skip list data structure
* Modular backend architecture

## Project Structure

```text
.
├── auth/        # Authentication and token handling
├── db/          # Core database logic
├── dbServer/    # HTTP server and request handling
├── jsondata/    # JSON value and visitor utilities
├── logger/      # Structured logging
├── patch/       # Document patch operations
├── schema/      # JSON schema validation
├── skiplist/    # Ordered in-memory skip list implementation
├── main.go      # Application entry point
├── go.mod       # Go module definition
└── go.sum       # Dependency lock file
```

## Getting Started

Clone the repository:

```bash
git clone https://github.com/muradImre/owldb-p1group12.git
cd owldb-p1group12
```

Install dependencies:

```bash
go mod tidy
```

Build the project:

```bash
go build -o owldb
```

Run the server:

```bash
./owldb -s document.json -t tokens.json -p 3318
```

You can also run the project directly with:

```bash
go run main.go -s document.json -t tokens.json -p 3318
```

## Command-Line Arguments

| Flag | Description                                    |
| ---- | ---------------------------------------------- |
| `-s` | Path to the JSON schema file                   |
| `-t` | Path to the token file used for authentication |
| `-p` | Port number for the database server            |

Example:

```bash
./owldb -s document.json -t tokens.json -p 3318
```

## Testing

Run all tests with:

```bash
go test ./...
```

## Notes

This repository is a public version of the OwlDB project. Runtime configuration files such as schemas and token files should be provided locally and should not contain private credentials or sensitive data.

