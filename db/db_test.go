package db

import (
	"context"
	"strings"
	"testing"

	"github.com/RICE-COMP318-FALL24/owldb-p1group12/skiplist"
)

// Helper function to create a document
func mockDocument(user string, data []byte) Document {
	return NewDocument(user, data)
}

// Basic test for deeply nested documents using the actual skiplist-backed DBIndex
func TestDeeplyNestedDocuments(t *testing.T) {
	// Factory function for skiplist-backed document collections
	collectionFactory := func() DBIndex[string, Document] {
		sl := skiplist.New[string, Document]("", strings.Repeat("\U0010FFFF", 2000))
		return sl
	}

	// Factory function for skiplist-backed collection sets
	collectionSetFactory := func() DBIndex[string, Collection] {
		sl := skiplist.New[string, Collection]("", strings.Repeat("\U0010FFFF", 2000))
		return sl
	}

	// Initialize the database with skiplist-backed collections
	database := New("testDB", collectionFactory, collectionSetFactory)
	// Create a nested structure: /doc1/collectionA/

	_, err := database.WriteDocument("user1", []string{"doc1"}, []byte("Top-level document data"), true)
	if err != nil {
		t.Fatalf("Error creating deeply nested document doc1: %v", err)
	}
	err = database.WriteCollection([]string{"doc1", "collectionA"})
	if err != nil {
		t.Fatalf("Error creating collectionA: %v", err)
	}
	_, err = database.WriteDocument("user1", []string{"doc1", "collectionA", "doc2"}, []byte("Nested document data 2"), true)
	if err != nil {
		t.Fatalf("Error creating deeply nested document doc2: %v", err)
	}
	err = database.WriteCollection([]string{"doc1", "collectionA", "doc2", "collectionB"})
	if err != nil {
		t.Fatalf("Error creating collectionA: %v", err)
	}

	// Test that overwriting and non-overwriting behavior works as expected; first create a document, then try inserting with
	// overwite off and check that it fails, then try inserting with overwrite on and check that it succeeds, both with bool and read
	_, err = database.WriteDocument("user1", []string{"doc1", "collectionA", "doc2", "collectionB", "doc3"}, []byte("Nested document data 3"), false)
	if err != nil {
		t.Fatalf("Error creating deeply nested document doc3: %v", err)
	}
	overwritten, err := database.WriteDocument("user1", []string{"doc1", "collectionA", "doc2", "collectionB", "doc3"}, []byte("Nested document data 3 NEW"), false)
	if err == nil && overwritten {
		t.Fatalf("Expected error and false when overwriting document with overwrite off, but got none or true")
	}
	doc, err := database.ReadDocument([]string{"doc1", "collectionA", "doc2", "collectionB", "doc3"})
	if err != nil {
		t.Fatalf("Error reading deeply nested document doc3: %v", err)
	}
	if string(doc.GetDocData()) != "Nested document data 3" {
		t.Fatalf("Expected 'Nested document data 3' for doc3, got %s", doc.GetDocData())
	}
	overwritten, err = database.WriteDocument("user1", []string{"doc1", "collectionA", "doc2", "collectionB", "doc3"}, []byte("Nested document data 3 NEW"), true)
	if err != nil {
		t.Fatalf("Error creating deeply nested document doc3: %v", err)
	}
	if !overwritten {
		t.Fatalf("Expected true when overwriting document with overwrite on, but got false")
	}
	doc, err = database.ReadDocument([]string{"doc1", "collectionA", "doc2", "collectionB", "doc3"})
	if err != nil {
		t.Fatalf("Error reading deeply nested document doc3: %v", err)
	}
	if string(doc.GetDocData()) != "Nested document data 3 NEW" {
		t.Fatalf("Expected 'Nested document data 3 NEW' for doc3, got %s", doc.GetDocData())
	}

}

// Test querying deeply nested collections using the actual skiplist-backed DBIndex
func TestQueryNestedCollections(t *testing.T) {
	// Factory function for skiplist-backed document collections
	collectionFactory := func() DBIndex[string, Document] {
		sl := skiplist.New[string, Document]("", strings.Repeat("\U0010FFFF", 2000))
		return sl
	}

	// Factory function for skiplist-backed collection sets
	collectionSetFactory := func() DBIndex[string, Collection] {
		sl := skiplist.New[string, Collection]("", strings.Repeat("\U0010FFFF", 2000))
		return sl
	}

	// Initialize the database with skiplist-backed collections
	database := New("testDB", collectionFactory, collectionSetFactory)

	_, err := database.WriteDocument("user1", []string{"doc1"}, []byte("Top-level document data"), true)
	if err != nil {
		t.Fatalf("Error creating doc1: %v", err)
	}
	err = database.WriteCollection([]string{"doc1", "collectionA"})
	if err != nil {
		t.Fatalf("Error creating collectionA: %v", err)
	}
	_, err = database.WriteDocument("user1", []string{"doc1", "collectionA", "doc2a"}, []byte("data2a"), true)
	if err != nil {
		t.Fatalf("Error writing document doc2a: %v", err)
	}
	_, err = database.WriteDocument("user2", []string{"doc1", "collectionA", "doc2b"}, []byte("data2b"), true)
	if err != nil {
		t.Fatalf("Error writing document docb: %v", err)
	}
	_, err = database.WriteDocument("user1", []string{"doc1", "collectionA", "doc2c"}, []byte("data2c"), true)
	if err != nil {
		t.Fatalf("Error writing document docc: %v", err)
	}
	_, err = database.WriteDocument("user1", []string{"doc1", "collectionA", "doc2d"}, []byte("data2d"), true)
	if err != nil {
		t.Fatalf("Error writing document docc: %v", err)
	}

	// Query for all documents in collectionB manually
	documents, err := database.ReadDocuments([]string{"doc1", "collectionA"}, "doc2a", "doc2c", context.Background())
	if err != nil {
		t.Fatalf("Error querying nested documents: %v", err)
	}
	if len(documents) != 3 {
		t.Fatalf("Expected 2 documents in collectionA, got %d", len(documents))
	}
	if string(documents["doc2a"].GetDocData()) != "data2a" {
		t.Fatalf("Expected 'data2a' for doc2a, got %s", documents["doc2a"].GetDocData())
	}
	if string(documents["doc2b"].GetDocData()) != "data2b" {
		t.Fatalf("Expected 'data2b' for doc2b, got %s", documents["doc2b"].GetDocData())
	}
	if string(documents["doc2c"].GetDocData()) != "data2c" {
		t.Fatalf("Expected 'data2c' for doc2c, got %s", documents["doc2c"].GetDocData())
	}

	// Query for all documents in collectionB by not specifying range
	documents, err = database.ReadDocuments([]string{"doc1", "collectionA"}, "", "", context.Background())
	if err != nil {
		t.Fatalf("Error querying nested documents: %v", err)
	}
	if len(documents) != 4 {
		t.Fatalf("Expected 4 documents in collectionA, got %d", len(documents))
	}
	if string(documents["doc2a"].GetDocData()) != "data2a" {
		t.Fatalf("Expected 'data2a' for doc2a, got %s", documents["doc2a"].GetDocData())
	}
	if string(documents["doc2b"].GetDocData()) != "data2b" {
		t.Fatalf("Expected 'data2b' for doc2b, got %s", documents["doc2b"].GetDocData())
	}
	if string(documents["doc2c"].GetDocData()) != "data2c" {
		t.Fatalf("Expected 'data2c' for doc2c, got %s", documents["doc2c"].GetDocData())
	}
	if string(documents["doc2d"].GetDocData()) != "data2d" {
		t.Fatalf("Expected 'data2d' for doc2d, got %s", documents["doc2d"].GetDocData())
	}
}

// Test reading a non-existent deeply nested document using the actual skiplist-backed DBIndex
func TestReadNonExistentNestedDocument(t *testing.T) {
	// Factory function for skiplist-backed document collections
	collectionFactory := func() DBIndex[string, Document] {
		sl := skiplist.New[string, Document]("", "\U0010FFFF")
		return sl
	}

	// Factory function for skiplist-backed collection sets
	collectionSetFactory := func() DBIndex[string, Collection] {
		sl := skiplist.New[string, Collection]("", "\U0010FFFF")
		return sl
	}

	// Initialize the database
	database := New("testDB", collectionFactory, collectionSetFactory)

	// Set up a deeply nested document structure
	_, err := database.WriteDocument("user1", []string{"doc1"}, []byte("Top-level document data"), true)
	if err != nil {
		t.Fatalf("Error creating doc1: %v", err)
	}

	err = database.WriteCollection([]string{"doc1", "collectionA"})
	if err != nil {
		t.Fatalf("Error creating collectionA: %v", err)
	}

	// Try to read a document that does not exist
	_, err = database.ReadDocument([]string{"doc1", "collectionA", "doc2"})
	if err == nil {
		t.Fatalf("Expected error when reading non-existent document, but got none")
	}

	// Try to read a deeply nested document from a collection that does not exist
	_, err = database.ReadDocument([]string{"doc1", "collectionB", "doc2"})
	if err == nil {
		t.Fatalf("Expected error when reading non-existent document, but got none")
	}

	// Try to read a deeply nested document where its outer containing document that does not exist
	_, err = database.ReadDocument([]string{"doc2", "collectionA", "doc2"})
	if err == nil {
		t.Fatalf("Expected error when reading non-existent document, but got none")
	}

	// Try to read deeply nested documentS from a collection that does not exist
	_, err = database.ReadDocuments([]string{"doc1", "collectionB"}, "", "", context.Background())
	if err == nil {
		t.Fatalf("Expected error when reading non-existent collection, but got none")
	}

	// Try to create a collection in a document that does not exist
	err = database.WriteCollection([]string{"doc2", "collectionA"})
	if err == nil {
		t.Fatalf("Expected error when creating collection in non-existent document, but got none")
	}

	// Try to create a document in a collection that does not exist
	_, err = database.WriteDocument("user1", []string{"doc1", "collectionB", "doc2"}, []byte("data2"), true)
	if err == nil {
		t.Fatalf("Expected error when creating document in non-existent collection, but got none")
	}

	// Try to read documents over a range that includes documents that do not exist (both start and end)
	// Add docs 2a, 2c, 2d, 2g, 2h; then try to read docs 2b, 2e, 2f
	_, err = database.WriteDocument("user1", []string{"doc1", "collectionA", "doc2a"}, []byte("data2a"), true)
	if err != nil {
		t.Fatalf("Error writing document doc2a: %v", err)
	}
	_, err = database.WriteDocument("user1", []string{"doc1", "collectionA", "doc2c"}, []byte("data2c"), true)
	if err != nil {
		t.Fatalf("Error writing document docc: %v", err)
	}
	_, err = database.WriteDocument("user1", []string{"doc1", "collectionA", "doc2d"}, []byte("data2d"), true)
	if err != nil {
		t.Fatalf("Error writing document docc: %v", err)
	}
	_, err = database.WriteDocument("user1", []string{"doc1", "collectionA", "doc2g"}, []byte("data2g"), true)
	if err != nil {
		t.Fatalf("Error writing document docc: %v", err)
	}
	_, err = database.WriteDocument("user1", []string{"doc1", "collectionA", "doc2h"}, []byte("data2h"), true)
	if err != nil {
		t.Fatalf("Error writing document docc: %v", err)
	}
	// Now try to read docs 2c, 2d using range 2b to 2f
	documents, err := database.ReadDocuments([]string{"doc1", "collectionA"}, "doc2b", "doc2f", context.Background())
	if err != nil {
		t.Fatalf("Error querying nested documents: %v", err)
	}
	if len(documents) != 2 {
		t.Fatalf("Expected 2 documents in collectionA, got %d", len(documents))
	}
	if string(documents["doc2c"].GetDocData()) != "data2c" {
		t.Fatalf("Expected 'data2c' for doc2c, got %s", documents["doc2c"].GetDocData())
	}
	if string(documents["doc2d"].GetDocData()) != "data2d" {
		t.Fatalf("Expected 'data2d' for doc2d, got %s", documents["doc2d"].GetDocData())
	}

}

// Test the DeleteDocument function
func TestDeleteDocument(t *testing.T) {
	collectionFactory := func() DBIndex[string, Document] {
		sl := skiplist.New[string, Document]("", strings.Repeat("\U0010FFFF", 2000))
		return sl
	}

	collectionSetFactory := func() DBIndex[string, Collection] {
		sl := skiplist.New[string, Collection]("", strings.Repeat("\U0010FFFF", 2000))
		return sl
	}

	// Initialize the database
	database := New("testDB", collectionFactory, collectionSetFactory)

	// Write a document and then delete it
	_, err := database.WriteDocument("user1", []string{"doc1"}, []byte("Top-level document data"), true)
	if err != nil {
		t.Fatalf("Error creating document doc1: %v", err)
	}
	err = database.DeleteDocument([]string{"doc1"})
	if err != nil {
		t.Fatalf("Error deleting document doc1: %v", err)
	}

	// Verify that the document no longer exists
	_, err = database.ReadDocument([]string{"doc1"})
	if err == nil {
		t.Fatalf("Expected error when reading deleted document, but got none")
	}
}

// Test the DeleteCollection function
func TestDeleteCollection(t *testing.T) {
	collectionFactory := func() DBIndex[string, Document] {
		sl := skiplist.New[string, Document]("", strings.Repeat("\U0010FFFF", 2000))
		return sl
	}

	collectionSetFactory := func() DBIndex[string, Collection] {
		sl := skiplist.New[string, Collection]("", strings.Repeat("\U0010FFFF", 2000))
		return sl
	}

	// Initialize the database
	database := New("testDB", collectionFactory, collectionSetFactory)

	_, err := database.WriteDocument("user1", []string{"doc1"}, []byte("Top-level document data"), true)
	if err != nil {
		t.Fatalf("Error creating document doc1: %v", err)
	}
	// Write a collection, then delete it
	err = database.WriteCollection([]string{"doc1", "collectionA"})
	if err != nil {
		t.Fatalf("Error creating collectionA: %v", err)
	}
	err = database.DeleteCollection([]string{"doc1", "collectionA"})
	if err != nil {
		t.Fatalf("Error deleting collectionA: %v", err)
	}

	// Verify that the collection no longer exists
	_, err = database.ReadDocuments([]string{"doc1", "collectionA"}, "", "", context.Background())
	if err == nil {
		t.Fatalf("Expected error when reading deleted collection, but got none")
	}
}
