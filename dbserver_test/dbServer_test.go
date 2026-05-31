package dbServer_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RICE-COMP318-FALL24/owldb-p1group12/db"
	"github.com/RICE-COMP318-FALL24/owldb-p1group12/dbServer"
	"github.com/RICE-COMP318-FALL24/owldb-p1group12/jsondata"
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
	am *MockAuthManager
}

func (f *AuthManagerFactory) NewAuthManager() dbServer.AuthService {
	return f.am
}

type UriResponseBody struct {
	Uri string `json:"uri"`
}

/*
Mock auth manager satisfying interface

	type AuthService interface {
		Login(w http.ResponseWriter, r *http.Request)           // Handles login and generates a token
		Logout(w http.ResponseWriter, r *http.Request)          // Handles logout and token invalidation
		ValidateToken(token string) (string, bool)              // Validates the token and checks for expiration
		HandlePreflight(w http.ResponseWriter, r *http.Request) // Handles CORS preflight requests
		GetUsernameFromRequest(r *http.Request) (string, error) // Extracts the username from the request

	}
*/
type MockAuthManager struct {
}

func (m *MockAuthManager) Login(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
func (m *MockAuthManager) Logout(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
func (m *MockAuthManager) ValidateToken(token string) (string, bool) {
	return "test", true
}
func (m *MockAuthManager) HandlePreflight(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
func (m *MockAuthManager) GetUsernameFromRequest(r *http.Request) (string, error) {
	return "user1", nil
}

// Create a server
func serverConstructor() *http.Server {
	// Initialize the AuthManager
	authManager := &MockAuthManager{}
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

	// Factory function for creating new skiplist of databases
	databasesFactory := func() dbServer.DBIndex[string, dbServer.DB] {
		sl := skiplist.New[string, dbServer.DB]("", strings.Repeat("\U0010FFFF", 2000))
		return sl
	}

	// Factory function for creating new databases
	dbFactory := func(name string) dbServer.DB {
		return db.New(name, collectionFactory, collectionSetFactory)
	}

	// Factory function for creating new validators using schema1 "schemas/schema1.json"
	sch, err := parser.SchemaParser("../schemas/schema1.json")
	if err != nil {
		panic(err)
	}
	validatorFactory := &SchemaValidatorFactory{schema: sch}

	// Factory function for creating new patch appliers
	patchApplier := patch.NewPatchApplier()

	// Create the server
	return dbServer.New(12345, authFactory, dbFactory, databasesFactory, validatorFactory, patchApplier)
}

func TestPutDB(t *testing.T) {
	handler := serverConstructor().Handler
	// Login
	req := httptest.NewRequest("POST", "/v1/login", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Test put DB when no DB exists
	req = httptest.NewRequest("PUT", "/v1/db1", nil)
	// Set the request header to include the token eg   -H 'Authorization: Bearer JReD5U0Hx3HotLbe'
	req.Header.Set("Authorization", "Bearer test")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	resp := w.Result()

	// Check the status code
	if resp.StatusCode != 201 {
		t.Errorf("Expected status code 201 but got %d", w.Code)
	}

	// Check the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Error reading response body: %v", err)
	}

	var respBody UriResponseBody
	err = json.Unmarshal(body, &respBody)
	if err != nil {
		t.Errorf("Error unmarshalling response body: %v", err)
	}

	// Make sure returned uri is correct
	if respBody.Uri != "/v1/db1" {
		t.Errorf("Expected value \"test todo\" but got %s", respBody.Uri)
	}

	// Validate location header
	locHeader := resp.Header.Get("Location")
	if locHeader != respBody.Uri {
		t.Errorf("Expected location header (%s) to match body URI (%s)", locHeader, "/v1/db1/doc1")
	}
	// Test put DB when a DB with the same name already exists
	req = httptest.NewRequest("PUT", "/v1/db1", nil)
	req.Header.Set("Authorization", "Bearer test")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	resp = w.Result()

	// Check the status code
	if resp.StatusCode != 400 {
		t.Errorf("Expected status code 400 but got %d", w.Code)
	}

	// Test put another unique DB when a DB exists already
	req = httptest.NewRequest("PUT", "/v1/db2", nil)
	req.Header.Set("Authorization", "Bearer test")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	resp = w.Result()

	// Check the status code
	if resp.StatusCode != 201 {
		t.Errorf("Expected status code 201 but got %d", w.Code)
	}
}

func TestSequentialDocument(t *testing.T) {
	handler := serverConstructor().Handler

	// Login
	req := httptest.NewRequest("POST", "/v1/login", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	req = httptest.NewRequest("PUT", "/v1/db1", nil)
	req.Header.Set("Authorization", "Bearer test")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Test put document when no document exists
	requestBody := `{
		"name": "John Doe",
		"age": 30
	}`
	req = httptest.NewRequest("PUT", "/v1/db1/doc1", strings.NewReader(requestBody))
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	resp := w.Result()

	if resp.StatusCode != 201 {
		t.Errorf("Expected status code 201 but got %d", w.Code)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Error reading response body: %v", err)
	}

	var respBody UriResponseBody
	err = json.Unmarshal(body, &respBody)
	if err != nil {
		t.Errorf("Error unmarshalling response body: %v", err)
	}

	if respBody.Uri != "/v1/db1/doc1" {
		t.Errorf("Expected value \"test todo\" but got %s", respBody.Uri)
	}

	locHeader := resp.Header.Get("Location")
	if locHeader != respBody.Uri {
		t.Errorf("Expected location header (%s) to match body URI (%s)", locHeader, "/v1/db1/doc1")
	}

	// Test put document when a document with the same name already exists

	modes := []string{"?mode=nooverwrite", "?mode=overwrite", ""}
	for _, mode := range modes {
		slog.Info("testing mode", "mode", mode)
		req = httptest.NewRequest("PUT", "/v1/db1/doc1"+mode, strings.NewReader(requestBody))
		req.Header.Set("Authorization", "Bearer test")
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		resp = w.Result()

		switch mode {
		case "?mode=nooverwrite":
			if resp.StatusCode != 412 {
				t.Errorf("Expected status code 412 on nooverwrite but got %d", w.Code)
			}
		default:
			if resp.StatusCode != 200 {
				t.Errorf("Expected status code 200 on overwrite but got %d", w.Code)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Errorf("Error reading response body: %v", err)
			}
			err = json.Unmarshal(body, &respBody)
			if err != nil {
				t.Errorf("Error unmarshalling response body: %v", err)
			}
			if respBody.Uri != "/v1/db1/doc1" {
				t.Errorf("Expected value \"/v1/db1/doc1\" but got %s", respBody.Uri)
			}
			locHeader := resp.Header.Get("Location")
			if locHeader != respBody.Uri {
				t.Errorf("Expected location header (%s) to match body URI (%s)", locHeader, "/v1/db1/doc1")
			}
		}
	}
	// Test putting a unique document in the same DB
	req = httptest.NewRequest("PUT", "/v1/db1/doc2", strings.NewReader(requestBody))
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	resp = w.Result()

	if resp.StatusCode != 201 {
		t.Errorf("Expected status code 201 but got %d", w.Code)
	}
}

func TestSequentialDatabase(t *testing.T) {
	handler := serverConstructor().Handler
	// Login
	req := httptest.NewRequest("POST", "/v1/login", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Test get database when the database doesn't exist
	req = httptest.NewRequest("GET", "/v1/db1/", nil)
	req.Header.Set("Authorization", "Bearer test")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	resp := w.Result()
	// Check code is 404
	if resp.StatusCode != 404 {
		t.Errorf("Expected status code 404 but got %d", w.Code)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/v1/db1", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	// Update request bodies to conform to the schema
	request1Body := `{
        "name": "John",
        "age": 25
    }`
	request2Body := `{
        "name": "Jane",
        "age": 30
    }`
	request3Body := `{
        "name": "Alice",
        "age": 22
    }`
	requestDocs := []string{request1Body, request2Body, request3Body}

	// Store expected documents in a map with path as the key
	expectedDocs := make(map[string]dbServer.ResponseStruct)
	for i, requestBody := range requestDocs {
		trimmedpath := "/doc" + strconv.Itoa(i)
		path := "/v1/db1/doc" + strconv.Itoa(i)
		req := httptest.NewRequest("PUT", path, strings.NewReader(requestBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test")
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		var reqBody dbServer.ResponseStruct
		err := json.Unmarshal([]byte(requestBody), &reqBody.Doc)
		if err != nil {
			t.Errorf("Error unmarshalling request body: %v", err)
		}
		expectedDocs[trimmedpath] = reqBody
	}

	// Store expected documents in a map with path as the key
	modes := []string{"?mode=nooverwrite", "?mode=overwrite", ""}
	for i, mode := range modes {
		slog.Info("testing mode", "mode", mode)
		req = httptest.NewRequest("PUT", "/v1/db1/doc0"+mode, strings.NewReader(request1Body))
		req.Header.Set("Authorization", "Bearer test")
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		resp = w.Result()

		switch modes[i] {
		case "?mode=nooverwrite":
			if resp.StatusCode != 412 {
				t.Errorf("Expected status code 412 on nooverwrite but got %d", w.Code)
			}
		default:
			if resp.StatusCode != 200 {
				t.Errorf("Expected status code 200 on overwrite but got %d", w.Code)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Errorf("Error reading response body: %v", err)
			}
			respBody := UriResponseBody{}
			slog.Info("body", "body", respBody)
			err = json.Unmarshal(body, &respBody)
			if err != nil {
				t.Errorf("Error unmarshalling response body: %v", err)
			}
			if respBody.Uri != "/v1/db1/doc0" {
				t.Errorf("Expected value \"/v1/db1/doc1\" but got %s", respBody.Uri)
			}
			locHeader := resp.Header.Get("Location")
			if locHeader != respBody.Uri {
				t.Errorf("Expected location header (%s) to match body URI (%s)", locHeader, "/v1/db1/doc1")
			}
		}
	}

	// Test get database when the database exists
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/v1/db1/", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status code 200 but got %d", w.Code)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Error reading response body: %v", err)
	}
	var respBody []dbServer.ResponseStruct
	err = json.Unmarshal(body, &respBody)
	if err != nil {
		t.Errorf("Error unmarshalling response body: %v", err)
	}
	slog.Info("respBody", "respBody", respBody)

	// Check path, doc/properties, and metadata for each document in the response
	for _, doc := range respBody {
		slog.Info("running loop...")
		// Check if the document path exists in the expectedDocs map
		expectedDoc, exists := expectedDocs[doc.Path]
		if !exists {
			t.Errorf("Unexpected document path: %s", doc.Path)
			continue
		}
		// Wrap both request and response documents in JSONValue to compare
		wrappedExpectedDoc, err := jsondata.NewJSONValue(expectedDoc.Doc)
		if err != nil {
			t.Errorf("Error creating JSONValue from expectedDoc: %v", err)
		}
		wrappedResponseBody, err := jsondata.NewJSONValue(doc.Doc)
		if err != nil {
			t.Errorf("Error creating JSONValue from respBody.Doc: %v", err)
		}
		if !doc.Doc.Equal(wrappedExpectedDoc) {
			t.Errorf("Expected value %s but got %s", wrappedExpectedDoc, wrappedResponseBody)
		}

		// Check metadata existence
		if doc.Meta.CreatedBy == "" {
			t.Errorf("Expected created by metadata to exist but got %s", doc.Meta.CreatedBy)
		}

		// Remove the document from the map once it's verified
		delete(expectedDocs, doc.Path)
		slog.Info("doc.Path", "doc.Path", doc.Path)
	}

	// Ensure no expected documents are missing from the response
	if len(expectedDocs) > 0 {
		t.Errorf("Missing documents in response: %v", expectedDocs)
	}

	// Now check over interval doc1, doc2
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/v1/db1/?interval=[doc1,doc2]", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status code 200 but got %d", w.Code)
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Error reading response body: %v", err)
	}
	err = json.Unmarshal(body, &respBody)
	if err != nil {
		t.Errorf("Error unmarshalling response body: %v", err)
	}

	// Check that only doc1 and doc2 are returned
	if len(respBody) != 2 {
		t.Errorf("Expected 2 documents but got %d", len(respBody))
	}
	for _, doc := range respBody {
		if doc.Path != "/doc1" && doc.Path != "/doc2" {
			t.Errorf("Unexpected document path: %s", doc.Path)
		}
	}

	// Now check interval with only min specified
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/v1/db1/?interval=[doc1,]", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected status code 200 but got %d", w.Code)
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Error reading response body: %v", err)
	}
	err = json.Unmarshal(body, &respBody)
	if err != nil {
		t.Errorf("Error unmarshalling response body: %v", err)
	}
	// Check that only doc2 and doc3 are returned
	if len(respBody) != 2 {
		t.Errorf("Expected 2 documents but got %d", len(respBody))
	}
	for _, doc := range respBody {
		if doc.Path != "/doc1" && doc.Path != "/doc2" {
			t.Errorf("Unexpected document path: %s", doc.Path)
		}
	}

	// Now check interval with only max specified
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/v1/db1/?interval=[,doc1]", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected status code 200 but got %d", w.Code)
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Error reading response body: %v", err)
	}
	err = json.Unmarshal(body, &respBody)
	if err != nil {
		t.Errorf("Error unmarshalling response body: %v", err)
	}
	// Check that only doc0 and doc1 are returned
	if len(respBody) != 2 {
		t.Errorf("Expected 2 documents but got %d", len(respBody))
	}
	for _, doc := range respBody {
		if doc.Path != "/doc0" && doc.Path != "/doc1" {
			t.Errorf("Unexpected document path: %s", doc.Path)
		}
	}

	// Now check interval with neither specified using [,]
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/v1/db1/?interval=[,]", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected status code 200 but got %d", w.Code)
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Error reading response body: %v", err)
	}
	err = json.Unmarshal(body, &respBody)
	if err != nil {
		t.Errorf("Error unmarshalling response body: %v", err)
	}
	// Check that all documents are returned
	if len(respBody) != 3 {
		t.Errorf("Expected 3 documents but got %d", len(respBody))
	}
	// Post a document to a database that doesn't exist
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/v1/db2/", strings.NewReader(request1Body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 404 {
		t.Errorf("Expected status code 404 but got %d", w.Code)
	}

	// Now try and post a document and ensure there are 4 total documents. note: name not specifyable, only collection name
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/v1/db1/", strings.NewReader(request1Body))
	req.Header.Set("Content-Type", "application/json")

	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 201 {
		t.Errorf("Expected status code 201 but got %d", w.Code)
	}

	// Ensure there are 4 documents now
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/v1/db1/", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected status code 200 but got %d", w.Code)
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Error reading response body: %v", err)
	}
	err = json.Unmarshal(body, &respBody)
	if err != nil {
		t.Errorf("Error unmarshalling response body: %v", err)
	}
	// Check that all documents are returned
	if len(respBody) != 4 {
		t.Errorf("Expected 4 documents but got %d", len(respBody))
	}
	// Now try and delete a document that doesn't exist
	w = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/v1/db1/doc4", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 404 {
		t.Errorf("Expected status code 404 but got %d", w.Code)
	}

	// Now try and delete a document
	w = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/v1/db1/doc1", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 204 {
		t.Errorf("Expected status code 204 but got %d", w.Code)
	}
	// Make sure it's gone
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/v1/db1/doc1", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 404 {
		t.Errorf("Expected status code 404 but got %d", w.Code)
	}

	// Now try and delete the database
	w = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/v1/db1", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 204 {
		t.Errorf("Expected status code 204 but got %d", w.Code)
	}
	// Make sure it's gone
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/v1/db1/", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 404 {
		t.Errorf("Expected status code 404 but got %d", w.Code)
	}

}

// Try posting collcections, then adding documents in collections
// Then try reading them with get on the collection path in db, posting to them,
// then deleting the collection and checking if the documents are gone
// collections are contained in documents, so must put a containing document first
func TestCollections(t *testing.T) {
	handler := serverConstructor().Handler
	// Login
	req := httptest.NewRequest("POST", "/v1/login", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	w = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/v1/db1", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)

	// Update request bodies to conform to the schema
	request1Body := `{
		"name": "John",
		"age": 25
	}`
	request2Body := `{
		"name": "Jane",
		"age": 30
	}`
	request3Body := `{
		"name": "Alice",
		"age": 22
	}`
	requestDocs := []string{request1Body, request2Body, request3Body}

	// Put containing outer document doc1 that will have the collection
	req = httptest.NewRequest("PUT", "/v1/db1/doc1", strings.NewReader(request1Body))
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	resp := w.Result()
	if resp.StatusCode != 201 {
		t.Errorf("Expected status code 201 but got %d", w.Code)
	}
	// Test get collection when no collection exists
	req = httptest.NewRequest("GET", "/v1/db1/doc1/col1/", nil)
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 404 {
		t.Errorf("Expected status code 404 but got %d", w.Code)
	}

	// Try and delete a collection that doesn't exist
	w = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/v1/db1/doc1/col1/", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 404 {
		t.Errorf("Expected status code 404 but got %d", w.Code)
	}

	// Put collection col1 in doc1
	req = httptest.NewRequest("PUT", "/v1/db1/doc1/col1/", nil)
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	resp = w.Result()

	if resp.StatusCode != 201 {
		t.Errorf("Expected status code 201 but got %d", w.Code)
	}

	// Store expected documents in a map with path as the key
	expectedDocs := make(map[string]dbServer.ResponseStruct)
	for i, requestBody := range requestDocs {
		trimmedpath := "/doc1/col1/doc" + strconv.Itoa(i)
		path := "/v1/db1/doc1/col1/doc" + strconv.Itoa(i)
		req := httptest.NewRequest("PUT", path, strings.NewReader(requestBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test")
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		var reqBody dbServer.ResponseStruct
		err := json.Unmarshal([]byte(requestBody), &reqBody.Doc)
		if err != nil {
			t.Errorf("Error unmarshalling request body: %v", err)
		}
		expectedDocs[trimmedpath] = reqBody
	}
	// Test get collection when the collection exists
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/v1/db1/doc1/col1/", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected status code 200 but got %d", w.Code)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Error reading response body: %v", err)
	}
	var respBody []dbServer.ResponseStruct
	err = json.Unmarshal(body, &respBody)
	if err != nil {
		t.Errorf("Error unmarshalling response body: %v", err)
	}
	slog.Info("respBody", "respBody", respBody)

	// Check path, doc/properties, and metadata for each document in the response
	for _, doc := range respBody {
		slog.Info("running loop...")
		// Check if the document path exists in the expectedDocs map
		expectedDoc, exists := expectedDocs[doc.Path]
		if !exists {
			t.Errorf("Unexpected document path: %s", doc.Path)
			continue
		}
		// Wrap both request and response documents in JSONValue to compare
		wrappedExpectedDoc, err := jsondata.NewJSONValue(expectedDoc.Doc)
		if err != nil {
			t.Errorf("Error creating JSONValue from expectedDoc: %v", err)
		}
		wrappedResponseBody, err := jsondata.NewJSONValue(doc.Doc)
		if err != nil {
			t.Errorf("Error creating JSONValue from respBody.Doc: %v", err)
		}
		if !doc.Doc.Equal(wrappedExpectedDoc) {
			t.Errorf("Expected value %s but got %s", wrappedExpectedDoc, wrappedResponseBody)
		}

		// Check metadata existence
		if doc.Meta.CreatedBy == "" {
			t.Errorf("Expected created by metadata to exist but got %s", doc.Meta.CreatedBy)
		}

		// Remove the document from the map once it's verified
		delete(expectedDocs, doc.Path)
		slog.Info("doc.Path", "doc.Path", doc.Path)
	}
	// Ensure no expected documents are missing from the response
	if len(expectedDocs) > 0 {
		t.Errorf("Missing documents in response: %v", expectedDocs)
	}
	// Now check over interval doc1, doc2
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/v1/db1/doc1/col1/?interval=[doc1,doc2]", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected status code 200 but got %d", w.Code)
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Error reading response body: %v", err)
	}
	err = json.Unmarshal(body, &respBody)
	if err != nil {
		t.Errorf("Error unmarshalling response body: %v", err)
	}
	// Check that only doc1 and doc2 are returned
	if len(respBody) != 2 {
		t.Errorf("Expected 2 documents but got %d", len(respBody))
	}
	for _, doc := range respBody {
		if doc.Path != "/doc1/col1/doc1" && doc.Path != "/doc1/col1/doc2" {
			t.Errorf("Unexpected document path: %s", doc.Path)
		}
	}

	// Now check interval with only min specified
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/v1/db1/doc1/col1/?interval=[doc1,]", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected status code 200 but got %d", w.Code)
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Error reading response body: %v", err)
	}
	err = json.Unmarshal(body, &respBody)
	if err != nil {
		t.Errorf("Error unmarshalling response body: %v", err)
	}
	// Check that only doc2 and doc3 are returned
	if len(respBody) != 2 {
		t.Errorf("Expected 2 documents but got %d", len(respBody))
	}
	for _, doc := range respBody {
		if doc.Path != "/doc1/col1/doc1" && doc.Path != "/doc1/col1/doc2" {
			t.Errorf("Unexpected document path: %s", doc.Path)
		}
	}

	// Now check interval with only max specified
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/v1/db1/doc1/col1/?interval=[,doc1]", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected status code 200 but got %d", w.Code)
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Error reading response body: %v", err)
	}
	err = json.Unmarshal(body, &respBody)
	if err != nil {
		t.Errorf("Error unmarshalling response body: %v", err)
	}
	// Check that only doc0 and doc1 are returned
	if len(respBody) != 2 {
		t.Errorf("Expected 2 documents but got %d", len(respBody))
	}
	for _, doc := range respBody {
		if doc.Path != "/doc1/col1/doc0" && doc.Path != "/doc1/col1/doc1" {
			t.Errorf("Unexpected document path: %s", doc.Path)
		}
	}

	// Now check interval with neither specified using [,]
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/v1/db1/doc1/col1/?interval=[,]", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected status code 200 but got %d", w.Code)
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Error reading response body: %v", err)
	}
	err = json.Unmarshal(body, &respBody)
	if err != nil {
		t.Errorf("Error unmarshalling response body: %v", err)
	}
	// Check that all documents are returned
	if len(respBody) != 3 {
		t.Errorf("Expected 3 documents but got %d", len(respBody))
	}
	// Try and post a document to a collection that doesn't exist
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/v1/db1/doc1/col2/", strings.NewReader(request1Body))
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 404 {
		t.Errorf("Expected status code 404 but got %d", w.Code)
	}

	// Now try and post a document and ensure there are 4 total documents. note: name not specifyable, only collection name
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/v1/db1/doc1/col1/", strings.NewReader(request1Body))
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 201 {
		t.Errorf("Expected status code 201 but got %d", w.Code)
	}
	// Ensure there are 4 document now
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/v1/db1/doc1/col1/", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected status code 200 but got %d", w.Code)
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Error reading response body: %v", err)
	}
	err = json.Unmarshal(body, &respBody)
	if err != nil {
		t.Errorf("Error unmarshalling response body: %v", err)
	}
	if len(respBody) != 4 {
		t.Errorf("Expected 4 documents but got %d", len(respBody))
	}

	// Now try and delete a document
	w = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/v1/db1/doc1/col1/doc1", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 204 {
		t.Errorf("Expected status code 204 but got %d", w.Code)
	}
	// Make sure it's gone
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/v1/db1/doc1/col1/doc1", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 404 {
		t.Errorf("Expected status code 404 but got %d", w.Code)
	}

	// Now try and delete the collection
	w = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/v1/db1/doc1/col1/", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 204 {
		t.Errorf("Expected status code 204 but got %d", w.Code)
	}
	// Make sure it's gone
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/v1/db1/doc1/col1/", nil)
	req.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != 404 {
		t.Errorf("Expected status code 404 but got %d", w.Code)
	}
}

func TestConcurrentRequests(t *testing.T) {
	handler := serverConstructor().Handler

	// Login first (shared by all goroutines)
	req := httptest.NewRequest("POST", "/v1/login", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Add Authorization header
	authHeader := "Bearer test"

	// Function to perform PUT on DB
	putDB := func(dbName string) {
		req := httptest.NewRequest("PUT", "/v1/"+dbName, nil)
		req.Header.Set("Authorization", authHeader)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		resp := w.Result()
		if resp.StatusCode != 201 && resp.StatusCode != 400 {
			t.Errorf("Expected 201 or 400 for PUT DB %s but got %d", dbName, resp.StatusCode)
		}
	}

	// Function to perform PUT document
	putDocument := func(dbName, docID, body string) {
		req := httptest.NewRequest("PUT", "/v1/"+dbName+"/"+docID, strings.NewReader(body))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		resp := w.Result()
		if resp.StatusCode != 201 && resp.StatusCode != 200 {
			t.Errorf("Expected 201 or 200 for PUT document %s in DB %s but got %d", docID, dbName, resp.StatusCode)
		}
	}

	// Function to perform GET document
	getDocument := func(dbName, docID string) {
		req := httptest.NewRequest("GET", "/v1/"+dbName+"/"+docID, nil)
		req.Header.Set("Authorization", authHeader)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		resp := w.Result()
		if resp.StatusCode != 200 && resp.StatusCode != 404 {
			t.Errorf("Expected 200 or 404 for GET document %s but got %d", docID, resp.StatusCode)
		}
	}

	// Function to perform DELETE document
	deleteDocument := func(dbName, docID string) {
		req := httptest.NewRequest("DELETE", "/v1/"+dbName+"/"+docID, nil)
		req.Header.Set("Authorization", authHeader)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		resp := w.Result()
		if resp.StatusCode != 204 {
			t.Errorf("Expected 204 for DELETE document %s but got %d", docID, resp.StatusCode)
		}
	}

	// Random document bodies
	docBodies := []string{
		`{"name": "John", "age": 25}`,
		`{"name": "Jane", "age": 30}`,
		`{"name": "Alice", "age": 22}`,
	}

	// Interleaved concurrent operations
	var wg sync.WaitGroup
	numGoroutines := 5
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			dbName := "db" + strconv.Itoa(i)
			docID := "doc" + strconv.Itoa(i)
			body := docBodies[i%len(docBodies)]

			// Simulate random operations
			putDB(dbName)
			putDocument(dbName, docID, body)
			getDocument(dbName, docID)
			deleteDocument(dbName, docID)
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Perform non-interleaved operations
	t.Log("Performing non-interleaved operations...")

	// Test sequential inserts, reads, and deletes
	dbName := "db_non_interleaved"
	putDB(dbName)

	// Insert multiple documents
	for i, body := range docBodies {
		docID := "doc" + strconv.Itoa(i)
		putDocument(dbName, docID, body)
	}

	// Read the documents sequentially
	for i := range docBodies {
		docID := "doc" + strconv.Itoa(i)
		getDocument(dbName, docID)
	}

	// Delete the documents sequentially
	for i := range docBodies {
		docID := "doc" + strconv.Itoa(i)
		deleteDocument(dbName, docID)
	}

	// Ensure no documents remain in the DB
	for i := range docBodies {
		docID := "doc" + strconv.Itoa(i)
		req := httptest.NewRequest("GET", "/v1/"+dbName+"/"+docID, nil)
		req.Header.Set("Authorization", authHeader)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		resp := w.Result()
		if resp.StatusCode != 404 {
			t.Errorf("Expected 404 for deleted document %s but got %d", docID, resp.StatusCode)
		}
	}
}

// get a random number of puts, then random number of deletes interleaved. then, ensure we get the exact number of
// puts as 201 status, and 0-random number of deletes number of 404, and 0 - # 404 number of 204s.

func TestConcurrentPutDelete(t *testing.T) {
	for i := 0; i < 3; i++ {
		handler := serverConstructor().Handler

		// Simulate concurrent operations with random PUTs and DELETEs
		var wg sync.WaitGroup
		rand.Seed(time.Now().UnixNano())

		// Store the number of successful PUTs and DELETEs
		var totalPuts atomic.Int32
		var total404Deletes, total204Deletes atomic.Int32
		totalPuts.Store(0)
		total404Deletes.Store(0)
		total204Deletes.Store(0)

		// Total operations, random PUTs and DELETEs
		numPuts := 25000
		numDeletes := 12500

		// Login first (shared by all goroutines)
		req := httptest.NewRequest("POST", "/v1/login", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		// First, create a database to put things into
		req = httptest.NewRequest("PUT", "/v1/db1", nil)
		req.Header.Set("Authorization", "Bearer test")
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		resp := w.Result()
		if resp.StatusCode != 201 {
			t.Errorf("Expected status code 201 but got %d", w.Code)
		}

		// Function to perform PUT document
		putDocument := func(dbName, docID, body string) {
			req := httptest.NewRequest("PUT", "/v1/"+dbName+"/"+docID, strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer test")
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			resp := w.Result()
			if resp.StatusCode == 201 {
				// Increment the total number of successful PUTs
				totalPuts.Add(1)
			}
		}

		// Function to perform DELETE document
		deleteDocument := func(dbName, docID string) {
			req := httptest.NewRequest("DELETE", "/v1/"+dbName+"/"+docID, nil)
			req.Header.Set("Authorization", "Bearer test")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			resp := w.Result()
			if resp.StatusCode == 404 {
				// Increment the total number of 404 DELETEs
				total404Deletes.Add(1)
			} else if resp.StatusCode == 204 {
				// Increment the total number of 204 DELETEs
				total204Deletes.Add(1)
			}
		}

		// Random document bodies
		docBodies := []string{
			`{"name": "John", "age": 25}`,
			`{"name": "Jane", "age": 30}`,
			`{"name": "Alice", "age": 22}`,
		}

		// Interleave concurrent operations with random PUTs and DELETEs
		for i := 0; i < numPuts+numDeletes; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()

				dbName := "db1"
				docID := "doc" + strconv.Itoa(i)
				body := docBodies[i%len(docBodies)]

				// Simulate random operations. 2/3 of time put, 1/3 of time delete
				if !(i%3 == 0) {
					putDocument(dbName, docID, body)
				} else {
					deleteDocument(dbName, docID)
				}
			}(i)
		}
		// Wait for all goroutines to complete
		wg.Wait()
		// Ensure the total number of successful PUTs is equal to the number of 201 status codes
		if totalPuts.Load() != int32(numPuts) {
			t.Errorf("Expected %d successful PUTs but got %d", numPuts, totalPuts.Load())
		}
		// Ensure the total number of 404 DELETEs is less than or equal to the number of DELETEs
		if total404Deletes.Load() > int32(numDeletes) {
			t.Errorf("Expected at most %d 404 DELETEs but got %d", numDeletes, total404Deletes.Load())
		}
		// Ensure the total number of 204 DELETEs is less than or equal to the number of DELETEs - 404 DELETEs
		if total204Deletes.Load() > int32(numDeletes)-total404Deletes.Load() {
			t.Errorf("Expected at most %d 204 DELETEs but got %d", numDeletes-int(total404Deletes.Load()), total204Deletes.Load())
		}
	}
}
