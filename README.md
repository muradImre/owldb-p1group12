[![Review Assignment Due Date](https://classroom.github.com/assets/deadline-readme-button-22041afd0340ce965d47ae6ef1cefeee28c7c493a6346c4f15d667ab976d596c.svg)](https://classroom.github.com/a/E9XQoq2H)
# OwlDB Project

OwlDB NoSQL document database for COMP 318.

## Getting started

Be sure to initialize your project using (where `<team>` is replaced
by your team's name):

```go mod init github.com/RICE-COMP318-FALL24/owldb-<team>```

You may then install the JSON schema package:

```go get github.com/santhosh-tekuri/jsonschema/v5```

Note the "v5" at the end.  This is very important.  You may only
use and import version 5 of this package.

Remember that you must **not** install any other external packages.

Be sure to commit the resulting `go.mod` and `go.sum` files.

## Provided Code

### main

The provided `main.go` file is a simple skeleton for you to start
with. It handles gracefully closing the HTTP server when Ctrl-C is
pressed in the terminal that is running your program.  It does little
else.

### jsondata

The provided `jsondata` package provides a `JSONValue` type, a
`Visitor` interface and a few basic functions to work with JSON data.
You **must** use this package whenever you access the contents of a
JSON document in your program.

### logger

The provided `logger` package provides a structured logger based on
the standard `log/slog` package that allows you to set the log level
and colorize the output.

## Build

Note that if you build your program as follows:

```go build```

The name of the executable will be `owldb-<team>` (where `<team>` is
replaced by your team's name).  If instead, you would like the name to
simply be `owldb`, you can do so as follows:

```go build -o owldb```

Assuming you have a file "document.json" that holds your desired
document schema and a file "tokens.json" that holds a set of tokens,
then you could run your program like so:

```./owldb -s document.json -t tokens.json -p 3318```

Note that you can always run your program without building it first as
follows:

```go run main.go -s document.json -t tokens.json -p 3318```

However, before you submit your project, always ensure that it runs
correctly using `go build`, as we will use `go build -o owldb` to
build your project.
