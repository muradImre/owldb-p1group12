// This package dbServer provides a database server for a server-client relationship
// with GET and PUT functionality for reading, writing, and storing JSON documents in
// database paths.
// ServerHandlers is a struct that contains the handlers for the server
package dbServer

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RICE-COMP318-FALL24/owldb-p1group12/db"
	"github.com/RICE-COMP318-FALL24/owldb-p1group12/jsondata"
	"github.com/RICE-COMP318-FALL24/owldb-p1group12/pair"
)

type AuthService interface {
	Login(w http.ResponseWriter, r *http.Request)           // Handles login and generates a token
	Logout(w http.ResponseWriter, r *http.Request)          // Handles logout and token invalidation
	ValidateToken(token string) (string, bool)              // Validates the token and checks for expiration
	HandlePreflight(w http.ResponseWriter, r *http.Request) // Handles CORS preflight requests
	GetUsernameFromRequest(r *http.Request) (string, error) // Extracts the username from the request
}

type Validator interface {
	ValidateDoc(document any) error
}

type DB interface {
	// WriteDocument writes a document to the database with the given user, name, data, and overwrite flag.
	// Returns true if successful, or false if the document exists and overwrite is false.
	WriteDocument(user string, path []string, data []byte, overwrite bool) (bool, error)
	// ReadDocument reads a document from the database using a given path.
	// Returns the Document if found or an error if not found.
	ReadDocument(path []string) (db.Document, error)
	// ReadDocuments returns a map of document names to their corresponding Document objects.
	ReadDocuments(path []string, min string, max string, ctx context.Context) (map[string]db.Document, error)
	// WriteCollection writes a collection to the database with a path
	WriteCollection(path []string) error
	// DeleteDocument deletes a document from the database using a given path.
	DeleteDocument(path []string) error
	// DeleteCollection deletes a collection from the database using a given path.
	DeleteCollection(path []string) error
}

// PatchApplier defines the interface for applying patches to docMap.
type PatchApplier interface {
	ApplyArrayAdd(docMap map[string]interface{}, path string, value interface{}) (bool, []byte, string)
	ApplyArrayRemove(docMap map[string]interface{}, path string, value interface{}) (bool, []byte, string)
	ApplyObjectAdd(docMap map[string]interface{}, path string, value interface{}) (bool, []byte, string)
}

type subscribers struct {
	mtx         sync.RWMutex
	subscribers map[string][]http.ResponseWriter // map of document paths to subscribers
}
type DBIndex[K cmp.Ordered, V any] interface {
	Upsert(key K, check func(key K, currValue V, exists bool) (newValue V, err error)) (updated bool, err error)
	Remove(key K) (removedValue V, removed bool)
	Find(key K) (foundValue V, found bool)
	Query(ctx context.Context, start K, end K) (results []pair.Pair[K, V], err error)
}

type serverHandlers struct {
	databases    DBIndex[string, DB]
	authManager  AuthService
	subscribers  subscribers
	validator    Validator
	patchApplier PatchApplier
}

// AuthFactory interface for creating a new authManager
type AuthFactory interface {
	NewAuthManager() AuthService
}

// DBFactory interface for creating a new database
type DBFactory func(name string) DB

var dbfactory DBFactory

type DatabasesFactory[K cmp.Ordered, V any] func() DBIndex[string, DB]

// Validator factory interface for creating a new validator
type ValidatorFactory interface {
	NewValidator() Validator
}

// Handle CORS preflight requests
func handlePreflight(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, DELETE, POST, OPTIONS, PATCH")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.WriteHeader(http.StatusOK)
}

// AuthorizationMiddleware checks the Authorization header and validates the token
func (h *serverHandlers) AuthorizationMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers in case of any error
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, DELETE, POST, OPTIONS, PATCH")
		w.Header().Set("Content-Type", "application/json")

		// Allow OPTIONS requests without authentication
		if r.Method == "OPTIONS" {
			handlePreflight(w)
			return
		}

		// Get the token from the Authorization header
		token := r.Header.Get("Authorization")
		if len(token) > 7 && strings.HasPrefix(token, "Bearer ") {
			token = strings.TrimPrefix(token, "Bearer ")
		} else {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode("unauthorized")
			return
		}

		// Validate the token
		_, valid := h.authManager.ValidateToken(token)
		if !valid {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode("unauthorized")
			return
		}

		// Token is valid, proceed to the next handler
		next(w, r)
	}
}

// Creates a new HTTP server with the given port and authManager
func New(port int, authfac AuthFactory, dbfac DBFactory, dbsfac DatabasesFactory[string, DB], valfac ValidatorFactory, patchApplier PatchApplier) *http.Server {
	mux := http.NewServeMux()
	handlers := serverHandlers{
		databases:    dbsfac(),
		authManager:  authfac.NewAuthManager(),
		validator:    valfac.NewValidator(),
		patchApplier: patchApplier,
		subscribers: subscribers{
			subscribers: make(map[string][]http.ResponseWriter), // Initialize the subscribers map
		},
	}
	dbfactory = dbfac
	// Authentication endpoints
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "OPTIONS":
			handlePreflight(w)
		case "POST":
			handlers.authManager.Login(w, r) // Handle login
		case "DELETE":
			handlers.authManager.Logout(w, r) // Handle logout
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Protected endpoints with Authorization Middleware
	mux.HandleFunc("/v1/", handlers.AuthorizationMiddleware(func(w http.ResponseWriter, r *http.Request) {

		switch r.Method {
		case "OPTIONS":
			handlePreflight(w)
		case "GET":
			handlers.handleGet(w, r)
		case "PUT":
			handlers.handlePut(w, r)
		case "POST":
			handlers.handlePost(w, r)
		case "PATCH":
			handlers.handlePatch(w, r)
		case "DELETE":
			handlers.handleDelete(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	// Start the server
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}
	return server
}

// handleGet handles GET requests for both databases and documents
func (h *serverHandlers) handleGet(w http.ResponseWriter, r *http.Request) {
	// Trim any trailing slashes to standardize the path
	isCollection := false
	// Check if trailing slash is present, indicating a collection
	if strings.HasSuffix(r.URL.Path, "/") {
		isCollection = true
	}
	path := strings.TrimSuffix(r.URL.Path, "/")
	parts := strings.Split(strings.Trim(path, "/"), "/")

	if len(parts) < 2 {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	slog.Debug("URL parts", "parts", parts)
	slog.Debug("len(parts)", "length", len(parts))

	intervalStr := r.URL.Query().Get("interval")
	slog.Debug("Interval string", "interval", intervalStr)
	var start, end string

	if intervalStr == "" || intervalStr == "[,]" {
		start = ""
		end = strings.Repeat("\U0010FFFF", 2000) // Max unicode string
	} else {
		slog.Info("Interval string", "interval", intervalStr)
		if intervalStr[0] != '[' || intervalStr[len(intervalStr)-1] != ']' {
			http.Error(w, "Invalid interval", http.StatusBadRequest)
			return
		}
		interval := strings.Split(intervalStr, ",")
		slog.Info("Interval", "interval", interval)
		// Remove leading and trailing []
		start = interval[0]
		end = interval[1]
		start = start[1:]
		end = end[:len(end)-1]
		slog.Info("Start", "start", start)
		slog.Info("End", "end", end)
	}

	if start > end && end != "" {
		http.Error(w, "Invalid interval", http.StatusBadRequest)
		return
	}

	// When end not specified, return all documents from start to the end of the collection
	if end == "" {
		end = strings.Repeat("\U0010FFFF", 2000) // Max unicode string
	}

	resourceType := ""
	if len(parts) == 2 {
		resourceType = "database"
	} else if len(parts)%2 == 1 {
		// Document-level request
		resourceType = "document"
	} else {
		// Collection-level request
		resourceType = "collection"
	}

	// Check for subscribe mode
	if r.URL.Query().Get("mode") == "subscribe" {
		documentPath := parts[2:] // Use documentPath from parts
		h.subscribe(w, r, resourceType, documentPath, start, end)
		return
	}

	// Determine the type of resource based on the number of parts and whether there's a trailing slash
	if len(parts) == 2 {
		if !isCollection {
			slog.Info("Requested database but passed in document")
			http.Error(w, "Invalid URL path", http.StatusBadRequest)
			return
		}
		slog.Info("Routing to getDatabase")
		h.getDatabase(w, r, start, end)
	} else if len(parts)%2 == 1 {
		if isCollection {
			slog.Info("Requested document but passed in collection")
			http.Error(w, "Invalid URL path", http.StatusBadRequest)
			return
		}
		slog.Info("Routing to getDocument")
		documentPath := parts[2:]
		h.getDocument(w, r, documentPath)
	} else {
		if !isCollection {
			slog.Info("Requested collection but passed in document")
			http.Error(w, "Invalid URL path", http.StatusBadRequest)
			return
		}
		slog.Info("Routing to getCollection")
		h.getCollection(w, r, start, end)
	}
}

// handlePut handles PUT requests for both databases and documents
func (h *serverHandlers) handlePut(w http.ResponseWriter, r *http.Request) {
	isCollection := false
	// Check if trailing slash is present, indicating a collection
	if strings.HasSuffix(r.URL.Path, "/") {
		isCollection = true
	}
	// Trim any trailing slashes to standardize the path
	path := strings.TrimSuffix(r.URL.Path, "/")
	parts := strings.Split(strings.Trim(path, "/"), "/")

	if len(parts) < 2 {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	slog.Info("URL parts", "parts", parts)

	// Determine the type of resource based on the number of parts and whether there's a trailing slash
	if len(parts) == 2 {
		slog.Info("Routing to putDatabase")
		// Hack as databases dont have trailing slash in put.
		if isCollection {
			slog.Info("Requested putDatabase but passed in document")
			http.Error(w, "Invalid URL path", http.StatusBadRequest)
			return
		}
		h.putDatabase(w, r)

	} else if len(parts)%2 == 1 {
		if isCollection {
			slog.Info("Requested putCollection but passed in document")
			http.Error(w, "Invalid URL path", http.StatusBadRequest)
			return
		}
		slog.Info("Routing to putDocument")
		documentPath := parts[2:]
		h.putDocument(w, r, documentPath)
	} else {
		if !isCollection {
			slog.Info("Requested putDocument but passed in collection")
			http.Error(w, "Invalid URL path", http.StatusBadRequest)
			return
		}
		slog.Info("Routing to putCollection")
		h.putCollection(w, r)
	}
}

// handlePost handles POST requests for both databases and documents
func (h *serverHandlers) handlePost(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "Invalid content type", http.StatusBadRequest)
		return
	}

	if len(parts) < 4 {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	dbName := parts[2]

	data, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}
	slog.Info("URL parts", "parts", parts)
	slog.Info("len(parts)", "length", len(parts))
	if len(parts) == 4 {
		slog.Info("Routing to postDocument (top-level)")
		h.postDocument(w, r, dbName, data)
	} else if len(parts)%2 == 0 {
		collectionPath := parts[3:]
		slog.Info("Routing to postCollection)", "collectionPath", collectionPath)
		h.postCollection(w, r, data)
	} else {
		http.Error(w, "Invalid URL path for document creation", http.StatusBadRequest)
		return
	}
}

// handlePost handles POST requests for both databases and documents
func (h *serverHandlers) handleDelete(w http.ResponseWriter, r *http.Request) {
	isCollection := false
	// Check if trailing slash is present, indicating a collection
	if strings.HasSuffix(r.URL.Path, "/") {
		isCollection = true
	}
	// Trim
	uri := strings.Trim(r.URL.Path, "/")
	slog.Info("handleDelete: uri:", "uri", uri)
	parts := strings.Split(uri, "/")
	if len(parts) < 2 {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	// Distinguish between database, document, and collection-level requests
	slog.Info("handleDelete: parts:", "parts", parts)
	if len(parts) == 2 {
		// Hack as databases dont have trailing slash in delete
		if isCollection {
			slog.Info("requested delete collection but passed in document")
			http.Error(w, "Invalid URL path", http.StatusBadRequest)
			return
		}
		// e.g., "/v1/comp318/" - database-level request
		slog.Info("routing to delete database..")
		h.deleteDatabase(w, r)
	} else if len(parts)%2 == 1 {
		if isCollection {
			slog.Info("requested delete collection but passed in document")
			http.Error(w, "Invalid URL path", http.StatusBadRequest)
			return
		}
		// e.g., "/v1/db1/doc1
		slog.Info("routing to delete document..")
		h.deleteDocument(w, r, parts)
	} else {
		if !isCollection {
			slog.Info("requested delete document but passed in collection")
			http.Error(w, "Invalid URL path", http.StatusBadRequest)
			return
		}
		// e.g., "/v1/db1/doc1/col1
		slog.Info("routing to delete collection..")
		h.deleteCollection(w, r, parts)
	}
}

// ResponseStruct holds the structure of what the document JSON data (including a documents meta data) would be.
type ResponseStruct struct {
	Path string             `json:"path"`
	Doc  jsondata.JSONValue `json:"doc"`
	Meta struct {
		CreatedAt      int64  `json:"createdAt"`
		CreatedBy      string `json:"createdBy"`
		LastModifiedAt int64  `json:"lastModifiedAt"`
		LastModifiedBy string `json:"lastModifiedBy"`
	} `json:"meta"`
}

// unMarshalDocument is a helper function that unmarshals a document and its metadata
func (h *serverHandlers) unMarshalDocument(document db.Document, path string) ResponseStruct {
	docData := document.GetDocData()
	docMetadata := document.GetDocMetadata()
	var jsonDoc jsondata.JSONValue
	err := json.Unmarshal(docData, &jsonDoc)
	if err != nil {
		slog.Error("Error unmarshalling document data", "error", err)
	}
	// Create the response struct
	response := ResponseStruct{
		Path: path,
		Doc:  jsonDoc,
	}

	// Fill in metadata information
	response.Meta.CreatedAt = docMetadata.CreatedAt()
	response.Meta.CreatedBy = docMetadata.CreatedBy()
	response.Meta.LastModifiedAt = docMetadata.LastModifiedAt()
	response.Meta.LastModifiedBy = docMetadata.LastModifiedBy()

	return response
}

// Getdocument retrives the documents from a database-document path, requested by the client
func (h *serverHandlers) getDocument(w http.ResponseWriter, r *http.Request, documentPath []string) error {

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return nil
	}

	dbName := parts[2]

	// Check if the database exists
	db, exists := h.databases.Find(dbName)
	if !exists {
		http.Error(w, "Database does not exist", http.StatusNotFound)
		return fmt.Errorf("Database does not exist")
	}

	// Check if the document exists
	document, err := db.ReadDocument(documentPath) // should read the path and return document
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return fmt.Errorf("error encoding response")
	}

	// Unmarshal the document and metadata
	path := strings.Join(parts[3:], "/")
	rawResponse := h.unMarshalDocument(document, "/"+path)

	responseData, err := json.Marshal(rawResponse)
	if err != nil {
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
		return fmt.Errorf("error encoding response")
	}

	// Write the response
	w.WriteHeader(http.StatusOK)
	w.Write(responseData)

	return nil
}

// PutDocument adds the documents onto a database-document path, given by the client
func (h *serverHandlers) putDocument(w http.ResponseWriter, r *http.Request, documentPath []string) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "Invalid content type", http.StatusBadRequest)
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	dbName := parts[2]

	data, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		http.Error(w, "Error reading body", http.StatusInternalServerError)
		return
	}

	var parsedDoc any
	if err := json.Unmarshal(data, &parsedDoc); err != nil {
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}

	err = h.validator.ValidateDoc(parsedDoc)
	if err != nil {
		slog.Error("Schema validation failed", "error", err)
		http.Error(w, "Invalid document schema", http.StatusBadRequest)
		return
	}

	// Check if the database exists
	db, exists := h.databases.Find(dbName)
	if !exists {
		http.Error(w, "Database does not exist", http.StatusNotFound)
		return
	}

	modeStr := r.URL.Query().Get("mode")
	slog.Info("modestr:", "value", modeStr)
	var overwrite bool
	if (modeStr == "overwrite") || (modeStr == "") {
		overwrite = true
	} else {
		if modeStr == "nooverwrite" {
			overwrite = false
		}
	}
	slog.Info("overwrite", "value", overwrite)

	// // need to get documentName from db
	// // need to give documentPath to db and get back documentName
	_, notExistErr := db.ReadDocument(documentPath)
	// if the document exists and overwrite is false, return 412
	if !overwrite && notExistErr == nil {
		http.Error(w, "Document already exists", http.StatusPreconditionFailed)
		return
	}

	// Retrieve the username from the authentication manager
	username, err := h.authManager.GetUsernameFromRequest(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	wrotedocument, err := db.WriteDocument(username, documentPath, data, overwrite) // need to check this for any bugs or erros when write works with full string and not string of just last elem
	if err != nil {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	if !overwrite {
		if !wrotedocument {
			http.Error(w, "Document already exists", http.StatusPreconditionFailed)
			return
		}
	}

	// write a response as a json object "{"uri": "/v1/my_new_database/my_new_document"}"
	response := map[string]string{
		"uri": "/v1/" + dbName + "/" + strings.Join(documentPath, "/"),
	}
	w.Header().Set("Location", response["uri"])

	exists = (notExistErr == nil)

	if overwrite && exists {
		w.WriteHeader(http.StatusOK)
		slog.Info("ovewritten")
	} else if overwrite && !exists {
		w.WriteHeader(http.StatusCreated)
		slog.Info("created")
	} else if !overwrite && !exists {
		w.WriteHeader(http.StatusCreated)
		slog.Info("created")
	}

	reponseBytes, err := json.Marshal(response)
	if err != nil {
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
		return
	}
	w.Write(reponseBytes)

	// document is the document data
	// documentPath is the path of the document
	document, _ := db.ReadDocument(documentPath)
	// Unmarshal the document data into docMap
	docData := document.GetDocData()
	var docMap map[string]interface{}
	if err := json.Unmarshal(docData, &docMap); err != nil {
		slog.Error("Error unmarshalling document data", "error", err)
		return
	}

	h.subscribeContent(documentPath, docMap, document, false)

}

// PostDocument adds the documents onto an arbitrary database-document path, given by the client
func (h *serverHandlers) postDocument(w http.ResponseWriter, r *http.Request, dbName string, data []byte) {

	var parsedDoc any
	if err := json.Unmarshal(data, &parsedDoc); err != nil {
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}

	err := h.validator.ValidateDoc(parsedDoc)
	if err != nil {
		slog.Error("Schema validation failed", "error", err)
		http.Error(w, "Invalid document schema", http.StatusBadRequest)
		return
	}

	// Check if the database exists
	db, exists := h.databases.Find(dbName)
	if !exists {
		http.Error(w, "Database does not exist", http.StatusNotFound)
		return
	}

	documentName := "doc" + fmt.Sprint(time.Now().UnixNano())

	// Retrieve the username from the authentication manager
	username, err := h.authManager.GetUsernameFromRequest(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	_, err = db.WriteDocument(username, []string{documentName}, data, true)
	if err != nil {
		http.Error(w, "Error writing/saving document", http.StatusInternalServerError)
		return
	}

	// write a response as a json object "{"uri": "/v1/my_new_database/my_new_document"}"
	response := map[string]string{
		"uri": "/v1/" + dbName + "/" + documentName,
	}
	w.Header().Set("Location", response["uri"])
	w.WriteHeader(http.StatusCreated)

	reponseBytes, err := json.Marshal(response)
	if err != nil {
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
		return
	}
	w.Write(reponseBytes)

	// documentPathWithDB := append([]string{dbName}, documentName)
	// h.subscribeContent(documentPathWithDB, parsedDoc, false)
}

func (h *serverHandlers) postCollection(w http.ResponseWriter, r *http.Request, data []byte) {
	var parsedDoc any
	if err := json.Unmarshal(data, &parsedDoc); err != nil {
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}

	err := h.validator.ValidateDoc(parsedDoc)
	if err != nil {
		slog.Error("Schema validation failed", "error", err)
		http.Error(w, "Invalid document schema", http.StatusBadRequest)
		return
	}

	// // Get the collection name from the URL
	path := strings.TrimSuffix(r.URL.Path, "/")
	parts := strings.Split(path, "/")

	if len(parts) < 4 {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	dbName := parts[2]
	collectionPath := parts[3:]
	containingPath := parts[3 : len(parts)-1]
	collectionName := parts[len(parts)-1]

	// Log the values for debugging
	slog.Info("Database Name", "dbName", dbName)
	slog.Info("Collection Path", "collectionPath", collectionPath)
	slog.Info("Containing Path", "containingPath", containingPath)
	slog.Info("Collection Name", "collectionName", collectionName)

	// Check if the database exists
	db, exists := h.databases.Find(dbName)
	if !exists {
		slog.Error("Database not found", "dbName", dbName)
		http.Error(w, "Database does not exist", http.StatusNotFound)
		return
	}
	slog.Info("Database found", "dbName", dbName)

	// Check if collection exists
	docs, err := db.ReadDocuments(append(containingPath, collectionName), "", "", context.Background())
	if err != nil {
		slog.Error("Error reading documents", "collectionPath", containingPath, "error", err)
		http.Error(w, "Collection does not exist", http.StatusNotFound)
		return
	}
	slog.Info("Documents found", "docCount", len(docs))

	documentName := "doc" + fmt.Sprint(time.Now().UnixNano())

	// Retrieve the username from the authentication manager
	username, err := h.authManager.GetUsernameFromRequest(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	_, err = db.WriteDocument(username, append(collectionPath, documentName), data, true)
	if err != nil {
		http.Error(w, "Error writing/saving document", http.StatusInternalServerError)
		return
	}

	// write a response as a json object "{"uri": "/v1/my_new_database/my_new_document"}"
	response := map[string]string{
		"uri": "/v1/" + dbName + "/" + strings.Join(append(collectionPath, documentName), "/"),
	}
	w.Header().Set("Location", response["uri"])
	w.WriteHeader(http.StatusCreated)

	reponseBytes, err := json.Marshal(response)
	if err != nil {
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
		return
	}
	w.Write(reponseBytes)

	slog.Info("Triggering SSE notification for collection", "documentPath", append(collectionPath, documentName))

	// documentPathWithDB := append([]string{dbName}, collectionPath...)
	// h.subscribeContent(documentPathWithDB, parsedDoc)

}

// GetDatabase is a handler that retrieves the database from a client request
func (h *serverHandlers) getDatabase(w http.ResponseWriter, r *http.Request, start string, end string) error {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	// Get the database name and document name from the URL
	parts := strings.Split(r.URL.Path, "/")

	if len(parts) < 3 {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return nil
	}

	dbName := parts[2]

	// Check if the database exists
	db, exists := h.databases.Find(dbName)

	if !exists {
		http.Error(w, "Database does not exist", http.StatusNotFound)
		return nil
	}

	docs, err := db.ReadDocuments([]string{}, start, end, r.Context())
	if err != nil {
		http.Error(w, "Error retrieving documents", http.StatusInternalServerError)
		return err
	}

	rawResponse := make([]ResponseStruct, len(docs))

	path := strings.Join(parts[3:], "/")
	i := 0
	for docName, doc := range docs {
		rawResponse[i] = h.unMarshalDocument(doc, path+"/"+docName)
		i++
	}
	slog.Info("added documents to response", "docCount", len(docs))

	// Convert the database to JSON and write it to the response
	response, err := json.Marshal(rawResponse)
	if err != nil {
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
		return err
	}
	w.Write(response)
	w.WriteHeader(http.StatusOK)

	return nil
}

func (h *serverHandlers) putCollection(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	// Get the database name from the URL
	path := strings.TrimSuffix(r.URL.Path, "/")
	parts := strings.Split(path, "/")

	dbName := parts[2]
	containingPath := parts[3 : len(parts)-1]
	collectionName := parts[len(parts)-1]

	slog.Info("Attempting to create collection", "dbName", dbName, "containingPath", containingPath, "collectionName", collectionName)
	if collectionName == "" {
		http.Error(w, "Invalid collection name", http.StatusBadRequest)
		return
	}

	slog.Info("Attempting to create collection", "dbName", dbName, "containingPath", containingPath, "collectionName", collectionName)

	// Check if containing doc exists
	db, exists := h.databases.Find(dbName)
	if !exists {
		slog.Error("Database not found", "dbName", dbName)
		http.Error(w, "Database does not exist", http.StatusNotFound)
		return
	}
	// Check if the collection already exists in the containing doc
	containingDoc, err := db.ReadDocument(containingPath)
	if err != nil {
		slog.Error("Error reading containing document", "containingPath", containingPath, "error", err)
		http.Error(w, "Document does not exist", http.StatusNotFound)
		return
	}
	_, exists = containingDoc.GetDocCollections().Find(collectionName)
	if exists {
		http.Error(w, "Collection already exists", http.StatusBadRequest)
		return
	}

	// Write the collection
	err = db.WriteCollection(parts[3:])
	if err != nil {
		http.Error(w, "Error writing collection", http.StatusInternalServerError)
		return
	}

	slog.Info("created collection", "collectionName", collectionName)
	slog.Info("dbname", "dbName", dbName)

	// write a response as a json object "{"uri": "/v1/my_new_database"}"
	response := map[string]string{
		"uri": "/v1/" + dbName + "/" + strings.Join(containingPath, "/") + "/" + collectionName + "/",
	}

	reponseBytes, err := json.Marshal(response)
	if err != nil {
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Location", response["uri"])
	w.WriteHeader(http.StatusCreated)
	w.Write(reponseBytes)
	slog.Info("Collection Created", "collectionName", collectionName)
}

// GetDatabase is a handler that retrieves the database from a client request
func (h *serverHandlers) getCollection(w http.ResponseWriter, r *http.Request, start string, end string) error {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	// Get the database and collection name from the URL
	path := strings.TrimSuffix(r.URL.Path, "/")
	parts := strings.Split(path, "/")

	if len(parts) < 5 {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return nil
	}

	dbName := parts[2]
	containingPath := parts[3 : len(parts)-1]
	collectionName := parts[len(parts)-1]

	slog.Info("Attempting to retrieve collection", "dbName", dbName, "containingPath", containingPath, "collectionName", collectionName)

	db, exists := h.databases.Find(dbName)
	if !exists {
		slog.Error("Database not found", "dbName", dbName)
		http.Error(w, "Database does not exist", http.StatusNotFound)
		return nil
	}
	slog.Info("Database found", "dbName", dbName)

	docs, err := db.ReadDocuments(append(containingPath, collectionName), start, end, r.Context())
	if err != nil {
		slog.Error("Error reading documents", "collectionPath", containingPath, "error", err)
		http.Error(w, "Collection does not exist", http.StatusNotFound)
		return err
	}
	slog.Info("Documents found", "docCount", len(docs))

	rawResponse := make([]ResponseStruct, len(docs))

	path = strings.Join(parts[3:], "/")
	i := 0
	for docName, doc := range docs {
		rawResponse[i] = h.unMarshalDocument(doc, "/"+path+"/"+docName)
		i++
	}

	// Convert the collection to JSON and write it to the response
	response, err := json.Marshal(rawResponse)
	if err != nil {
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
		return err
	}
	w.Write(response)
	w.WriteHeader(http.StatusOK)

	// liveDocument := count.status(document) // get the status of this document
	// if liveDocument {
	// 	count.New().send( /*send over the change or somthing so it can print it */ )
	// }
	return nil
}

// deleteCollection deletes a collection from the database using a given path.
func (h *serverHandlers) deleteCollection(w http.ResponseWriter, r *http.Request, path []string) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	// Get the database and collection name from the URL
	// Example valid parts: [v1 db doc1 col1]
	parts := path
	if len(parts) < 4 {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	dbName := parts[1]
	db, exists := h.databases.Find(dbName)
	if !exists {
		http.Error(w, "Database does not exist", http.StatusNotFound)
		return
	}
	err := db.DeleteCollection(parts[2:])
	if err != nil {
		http.Error(w, "Collection not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	slog.Info(fmt.Sprintf("Collection %s Deleted", parts[len(parts)-1]))
}

// Putdatabase adds a database onto into the server, given by the client
func (h *serverHandlers) putDatabase(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	// Get the database name from the URL
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 3 {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	dbName := parts[2]

	// Check if the database already exists
	if _, exists := h.databases.Find(dbName); exists {
		http.Error(w, "Database already exists", http.StatusBadRequest)
		return
	}
	newDB := dbfactory(dbName)
	// Add the database to the holder using upsert with check function defined such that fails if exists already
	// Check func takes form check func(key K, currValue V, exists bool) (newValue V, err error)
	_, err := h.databases.Upsert(dbName, func(key string, currValue DB, exists bool) (DB, error) {
		if exists {
			return nil, fmt.Errorf("Database already exists")
		}
		return newDB, nil
	})
	if err != nil {
		http.Error(w, "Error creating database", http.StatusBadRequest)
		return
	}
	finalDB, _ := h.databases.Find(dbName)
	slog.Info("Database Created", "dbName", finalDB)
	slog.Info("Database Created", "dbName", dbName)

	// write a response as a json object "{"uri": "/v1/my_new_database"}"
	response := map[string]string{
		"uri": "/v1/" + dbName,
	}
	reponseBytes, err := json.Marshal(response)
	if err != nil {
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Location", response["uri"])
	w.WriteHeader(http.StatusCreated)
	w.Write(reponseBytes)
	slog.Info("Database Created", "dbName", dbName)
}

func (h *serverHandlers) deleteDatabase(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	// Get the database name from the URL
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 3 {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	dbName := parts[2]

	// Check if the database already exists
	if _, exists := h.databases.Find(dbName); !exists {
		http.Error(w, "Database does not exists", http.StatusNotFound)
		return
	}

	// remove it from the holder.
	h.databases.Remove(dbName)

	slog.Info("Database Deleted", "dbName", dbName)

	w.WriteHeader(http.StatusNoContent)
}

func (h *serverHandlers) deleteDocument(w http.ResponseWriter, r *http.Request, path []string) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	// Get the database name from the URL
	// Example valid parts: [v1 db doc1]

	parts := path
	if len(parts) < 3 {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	dbName := parts[1]

	// Check if the database already exists
	db, exists := h.databases.Find(dbName)
	if !exists {
		http.Error(w, "Database does not exist", http.StatusNotFound)
		return
	}
	// Check if document exists using readdocument
	_, err := db.ReadDocument(parts[2:])
	if err != nil {
		http.Error(w, "Document does not exist", http.StatusNotFound)
		return
	}

	err = db.DeleteDocument(parts[2:])
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	// Notify subscribers about the deletion
	document, _ := db.ReadDocument(parts[2:])
	h.subscribeContent(parts[2:], nil, document, true) // Pass the path without "v1"

	slog.Info(fmt.Sprintf("Document %s Deleted", parts[len(parts)-1]))
	w.WriteHeader(http.StatusNoContent)

}

// handlePatch handles PATCH requests to update documents atomically
func (h *serverHandlers) handlePatch(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "Invalid content type", http.StatusBadRequest)
		return
	}

	slog.Info("Handling PATCH request")
	// Parse document path from the URL
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	dbName := parts[2]
	documentPath := parts[3:]
	log.Println("documentPath", documentPath)

	// Retrieve the database
	slog.Info("Retrieving database", "dbName", dbName)
	db, exists := h.databases.Find(dbName)
	if !exists {
		http.Error(w, "Database does not exist", http.StatusNotFound)
		return
	}
	slog.Info("Database retrieved successfully", "dbName", dbName)

	// Retrieve the document
	slog.Info("Retrieving document", "documentPath", documentPath)
	document, err := db.ReadDocument(documentPath)
	if err != nil {
		http.Error(w, "Document does not exist", http.StatusNotFound)
		return
	}
	slog.Info("Document retrieved successfully", "documentPath", documentPath)

	// Parse the patches array from the request body
	var patches []map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&patches); err != nil {
		http.Error(w, "Invalid patches format", http.StatusBadRequest)
		return
	}
	slog.Info("Patches decoded successfully")

	// Load the document data into an editable map
	docMap := make(map[string]interface{})
	if err := json.Unmarshal(document.GetDocData(), &docMap); err != nil {
		http.Error(w, "Error unmarshalling document data", http.StatusInternalServerError)
		return
	}
	slog.Info("Document data unmarshalled successfully")

	// Apply patches sequentially and keep track of the document state
	patchFailed := false
	message := "patch applied"
	for _, patchOp := range patches {
		op, opOk := patchOp["op"].(string)
		path, pathOk := patchOp["path"].(string)
		value, valueOk := patchOp["value"]

		// Validate patch operation structure
		if !opOk || !pathOk || !valueOk {
			http.Error(w, "Malformed patch operation", http.StatusBadRequest)
			return
		}

		// Apply patch based on operation type
		switch op {
		case "ArrayAdd":
			patchFailed, _, message = h.patchApplier.ApplyArrayAdd(docMap, path, value)
		case "ArrayRemove":
			patchFailed, _, message = h.patchApplier.ApplyArrayRemove(docMap, path, value)
		case "ObjectAdd":
			patchFailed, _, message = h.patchApplier.ApplyObjectAdd(docMap, path, value)
		default:
			patchFailed = true
			message = fmt.Sprintf("Invalid patch operation: %s", op)
		}

		// Stop if a patch fails to avoid partially applied patches
		if patchFailed {
			response := map[string]interface{}{
				"uri":         fmt.Sprintf("/v1/%s/%s", dbName, strings.Join(documentPath, "/")),
				"patchFailed": patchFailed,
				"message":     message,
			}
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(response)
			return
		}
	}

	// Validate the final document state against the schema
	slog.Info("Validating final document state against schema")
	if err := h.validator.ValidateDoc(docMap); err != nil {
		slog.Error("Schema validation failed after patch operations", "error", err)
		http.Error(w, "Patching caused schema violation: "+err.Error(), http.StatusBadRequest)
		return
	}
	slog.Info("Document state validated successfully")

	// Marshal the final state and save it to the database if patches succeeded
	finalData, err := json.Marshal(docMap)
	if err != nil {
		http.Error(w, "Error encoding patched document", http.StatusInternalServerError)
		return
	}
	slog.Info("Final document state marshalled successfully")

	// Save the patched document to the database
	username, err := h.authManager.GetUsernameFromRequest(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if _, err := db.WriteDocument(username, documentPath, finalData, true); err != nil {
		http.Error(w, "Error saving patched document", http.StatusInternalServerError)
		return
	}
	slog.Info("Patched document saved successfully")

	// Construct and send success response
	response := map[string]interface{}{
		"uri":         fmt.Sprintf("/v1/%s/%s", dbName, strings.Join(documentPath, "/")),
		"patchFailed": false,
		"message":     message,
	}
	// Notify subscribers of update
	slog.Info("Triggering SSE notification for document", "documentPath", documentPath)
	document, _ = db.ReadDocument(documentPath)

	h.subscribeContent(documentPath, docMap, document, false)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)

}

// func (s *serverHandlers) CountHandler(mux *http.ServeMux) {
// 	mux.Handle("/count", count.New())
// }

func (h *serverHandlers) subscribe(w http.ResponseWriter, r *http.Request, resourceType string, documentPath []string, start string, end string) {
	type writeFlusher interface {
		http.ResponseWriter
		http.Flusher
	}

	wf, ok := w.(writeFlusher)
	if !ok {
		slog.Error("Streaming unsupported on this connection")
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	slog.Info("Starting subscription", "documentPath", documentPath)

	// Set up event stream connection
	wf.Header().Set("Content-Type", "text/event-stream")
	wf.Header().Set("Cache-Control", "no-cache")
	wf.Header().Set("Connection", "keep-alive")
	wf.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Last-Event-ID")
	wf.Header().Set("Access-Control-Allow-Origin", "*")
	wf.WriteHeader(http.StatusOK)
	wf.Flush()
	slog.Info("SSE headers sent")

	switch resourceType {
	case "document":
		err := h.getDocument(w, r, documentPath)
		if err != nil {
			slog.Error("Error getting document during subscription", "error", err)
			return
		}
		h.addSubscriber(documentPath, wf, false, start, end)
		slog.Info("Subscriber added", "documentPath", documentPath)

		// **Trigger Immediate Notification**
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) >= 3 {
			dbName := parts[1] // Database name is the second element in the URL path
			db, exists := h.databases.Find(dbName)
			slog.Info("exists", "value", exists)
			if exists {
				document, err := db.ReadDocument(documentPath)
				if err == nil {
					docData := make(map[string]interface{})
					if err := json.Unmarshal(document.GetDocData(), &docData); err == nil {
						slog.Info("Triggering immediate notification for document", "documentPath", documentPath)
						h.subscribeContent(documentPath, docData, document, false)
					}
				}
			}
		}
	case "collection":
		err := h.getCollection(w, r, start, end)
		if err != nil {
			slog.Error("Error getting document during subscription", "error", err)
			return
		}
		slog.Info("Subscribing with documentPath", "documentPath", documentPath)
		h.addSubscriber(documentPath, wf, true, start, end)
		slog.Info("Subscriber added", "documentPath", documentPath)
	case "database":
		err := h.getDatabase(w, r, start, end)
		if err != nil {
			slog.Error("Error getting db during subscription", "error", err)
			return
		}
		parts := strings.Split(r.URL.Path, "/")
		h.addSubscriber([]string{parts[2]}, wf, true, start, end)
	}

	for {
		select {
		case <-r.Context().Done():
			// Client closed connection
			slog.Info("Client closed connection")
			return
		case <-time.After(15 * time.Second):
			// Send a keep-alive message every 15 seconds
			wf.Write([]byte(": keep-alive\n\n"))
			wf.Flush()
			slog.Info("Keep-alive message sent")
		}
	}
}

// subscribeContent sends a notification to all subscribers for a given document path
func (h *serverHandlers) subscribeContent(documentPath []string, docMap map[string]interface{}, document db.Document, isDelete bool) {
	slog.Info("subscribeContent called", "documentPath", documentPath, "document", document)

	// Current timestamp in milliseconds since Unix epoch
	eventID := time.Now().UnixMilli()
	path := "/" + strings.Join(documentPath, "/")
	eventType := "update"

	// Prepare event data based on whether it's an update or delete
	var eventData string
	if isDelete {
		// If it's a delete event
		eventType = "delete"
		eventData = fmt.Sprintf(`"%s"`, path) // Quoted path for delete events
	} else {
		// If there's document data, it's an update event
		docMetadata := document.GetDocMetadata()
		eventDataBytes, err := json.Marshal(map[string]interface{}{
			"path": path,
			"doc":  docMap,
			"meta": map[string]interface{}{
				"createdAt":      docMetadata.CreatedAt(),
				"createdBy":      docMetadata.CreatedBy(),
				"lastModifiedAt": docMetadata.LastModifiedAt(),
				"lastModifiedBy": docMetadata.LastModifiedBy(),
			},
		})
		if err != nil {
			slog.Error("Error encoding document data for SSE", "error", err)
			return
		}
		eventData = string(eventDataBytes)
	}

	// Construct the event string with appropriate format
	slog.Info("Constructing event string", "eventType", eventType)
	eventString := fmt.Sprintf("event: %s\ndata: %s\nid: %d\n\n", eventType, eventData, eventID)

	// Fetch subscribers for the exact document path
	subscribers := h.getSubscribers(documentPath)
	if len(subscribers) == 0 {
		slog.Warn("No subscribers found for exact document path", "path", path)
	} else {
		slog.Info("Notifying subscribers for document", "count", len(subscribers))
	}

	// Notify exact path subscribers
	for _, subscriber := range subscribers {
		slog.Info("Sending notification to subscriber", "path", path)
		subscriber.Write([]byte(eventString))
		subscriber.(http.Flusher).Flush()
	}

	// Notify collection-level subscribers
	if len(documentPath) > 1 {
		collectionPath := strings.Join(documentPath[:len(documentPath)-1], "/")
		collectionSubscribers := h.getSubscribers(documentPath[:len(documentPath)-1])
		if len(collectionSubscribers) == 0 {
			slog.Warn("No subscribers found for collection path", "collectionPath", collectionPath)
		} else {
			slog.Info("Notifying subscribers for collection", "count", len(collectionSubscribers))
		}

		for _, subscriber := range collectionSubscribers {
			slog.Info("Sending notification to collection subscriber", "path", collectionPath)
			subscriber.Write([]byte(eventString))
			subscriber.(http.Flusher).Flush()
		}
	}

	slog.Info("Notification sent to all subscribers", "path", path)
}

func (h *serverHandlers) getSubscribers(documentPath []string) []http.ResponseWriter {
	h.subscribers.mtx.RLock()
	defer h.subscribers.mtx.RUnlock()
	path := strings.Join(documentPath, "/")
	slog.Info("Fetching subscribers for path", "path", path)

	// Log all registered subscribers for debugging purposes
	for registeredPath := range h.subscribers.subscribers {
		slog.Info("Registered subscriber path", "registeredPath", registeredPath)
	}

	return h.subscribers.subscribers[path]
}

// addSubscriber adds a new subscriber to the list of subscribers for a given document path
func (h *serverHandlers) addSubscriber(documentPath []string, wf http.ResponseWriter, collection bool, start string, end string) {
	h.subscribers.mtx.Lock()
	defer h.subscribers.mtx.Unlock()

	path := strings.Join(documentPath, "/")
	slog.Info("Attempting to add subscriber", "path", path)

	// Ensure the map is initialized
	if h.subscribers.subscribers == nil {
		h.subscribers.subscribers = make(map[string][]http.ResponseWriter)
	}

	// Remove any existing subscribers for this path
	h.subscribers.subscribers[path] = []http.ResponseWriter{}

	if collection {
		slog.Info("Handling collection subscription", "path", path)

		if len(documentPath) == 1 {
			// If subscribing to the whole database
			dbName := documentPath[0]
			_, exists := h.databases.Find(dbName)
			if !exists {
				slog.Error("Database does not exist", "dbName", dbName)
				return
			}

			// Add the subscriber to the database-level events
			h.subscribers.subscribers[path] = append(h.subscribers.subscribers[path], wf)
			slog.Info("Subscriber added to database", "path", path)
		} else {
			// Handle collection-level subscriptions
			dbName := documentPath[0]
			db, exists := h.databases.Find(dbName)
			if !exists {
				slog.Error("Database does not exist", "dbName", dbName)
				return
			}

			docs, err := db.ReadDocuments(documentPath[1:], start, end, context.Background())
			if err != nil {
				slog.Error("Error reading collection for subscription", "error", err)
				return
			}

			for docName := range docs {
				docPath := path + "/" + docName
				slog.Info("Adding subscriber to document in collection", "docName", docName, "docPath", docPath)
				if h.subscribers.subscribers[docPath] == nil {
					h.subscribers.subscribers[docPath] = []http.ResponseWriter{}
				}
				h.subscribers.subscribers[docPath] = append(h.subscribers.subscribers[docPath], wf)
				slog.Info("Subscriber added to document in collection", "docPath", docPath)
			}
		}
	} else {
		// Handle document-level subscriptions
		slog.Info("Adding subscriber for document", "path", path)

		h.subscribers.subscribers[path] = append(h.subscribers.subscribers[path], wf)
		slog.Info("Subscriber added for document", "path", path)
	}
}
