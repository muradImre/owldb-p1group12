// Main package for the OwlDB nosql server
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/RICE-COMP318-FALL24/owldb-p1group12/auth"
	"github.com/RICE-COMP318-FALL24/owldb-p1group12/db"
	"github.com/RICE-COMP318-FALL24/owldb-p1group12/dbServer"
	"github.com/RICE-COMP318-FALL24/owldb-p1group12/patch"
	"github.com/RICE-COMP318-FALL24/owldb-p1group12/schema/parser"
	"github.com/RICE-COMP318-FALL24/owldb-p1group12/schema/validator"
	"github.com/RICE-COMP318-FALL24/owldb-p1group12/skiplist"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

type SchemaValidatorFactory struct {
	schema *jsonschema.Schema
}

func (f *SchemaValidatorFactory) NewValidator() dbServer.Validator {
	return validator.New(f.schema)
}

type AuthManagerFactory struct {
	am *auth.AuthManager
}

func (f *AuthManagerFactory) NewAuthManager() dbServer.AuthService {
	return f.am
}

func main() {
	var server http.Server
	var port int
	var fileschema, filetoken string
	var sch *jsonschema.Schema
	var debug bool = false

	var err error
	flag.IntVar(&port, "p", 3318, "help message for flag n -- port")
	flag.StringVar(&fileschema, "s", "document-schema.json", "help message for flag s -- validate all documents stored in your database")
	flag.StringVar(&filetoken, "t", "testing-tokens.json", "help message for flag t -- mapping string user names to string tokens")
	// Add a flag for debug mode; not needed; just for testing. If set, it will set debug to true and print debug messages
	flag.BoolVar(&debug, "d", false, "help message for flag d -- debug mode")

	flag.Parse()

	if debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	} else {
		slog.SetLogLoggerLevel(slog.LevelInfo)
	}

	//parse schema from schema/parser
	sch, err = parser.SchemaParser(fileschema)
	if err != nil {
		slog.Error("Error parsing arguments", "error", err)
		return
	}

	// Load the token file
	existingTokens := make(map[string]string)
	tokenFile, err := os.ReadFile(filetoken)
	if err == nil {
		// If file exists, parse the token file
		err := json.Unmarshal(tokenFile, &existingTokens)
		if err != nil {
			slog.Error("Error parsing token file", "error", err)
		}
	} else {
		// If the file doesn't exist, log and proceed
		fmt.Printf("Token file not found. New tokens will be generated.\n")
	}

	// existingTokens are now token: username, so we need to reverse it to username: token
	reversedTokens := make(map[string]string)
	for token, username := range existingTokens {
		reversedTokens[username] = token
	}
	existingTokens = reversedTokens

	// Initialize the AuthManager
	authManager := auth.NewAuthManager(existingTokens)
	// Set expiration time for existing tokens to be 24 hours
	authManager.SetExistingTokens()
	authFactory := &AuthManagerFactory{am: authManager}

	// Define a skiplist-backed collection factory
	collectionFactory := func() db.DBIndex[string, db.Document] {
		sl := skiplist.New[string, db.Document]("", strings.Repeat("\U0010FFFF", 2000))
		return sl
	}

	// Factory function for skiplist-backed collection sets
	collectionSetFactory := func() db.DBIndex[string, db.Collection] {
		sl := skiplist.New[string, db.Collection]("", strings.Repeat("\U0010FFFF", 2000))
		return sl
	}

	// Factory function for creating new databases
	dbFactory := func(name string) dbServer.DB {
		return db.New(name, collectionFactory, collectionSetFactory)
	}

	// Factory function for creating new validators
	validatorFactory := &SchemaValidatorFactory{schema: sch}

	// Factory function for creating new patch appliers
	patchApplier := patch.NewPatchApplier()

	// Factory function for creating new skiplist of databases
	databasesFactory := func() dbServer.DBIndex[string, dbServer.DB] {
		sl := skiplist.New[string, dbServer.DB]("", strings.Repeat("\U0010FFFF", 2000))
		return sl
	}

	// Create the server
	server = *dbServer.New(port, authFactory, dbFactory, databasesFactory, validatorFactory, patchApplier)
	// server.CountHandler(mux)
	// server.Handler = mux

	// The following code should go last and remain unchanged.
	// Note that you must actually initialize 'server' and 'port'
	// before this.  Note that the server is started below by
	// calling ListenAndServe.  You must not start the server
	// before this.

	// signal.Notify requires the channel to be buffered
	ctrlc := make(chan os.Signal, 1)
	signal.Notify(ctrlc, os.Interrupt, syscall.SIGTERM)
	go func() {
		// Wait for Ctrl-C signal
		<-ctrlc
		server.Close()
	}()

	// Start server
	slog.Info("Listening", "port", port)
	err = server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		slog.Error("Server closed", "error", err)
	} else {
		slog.Info("Server closed", "error", err)
	}
}
