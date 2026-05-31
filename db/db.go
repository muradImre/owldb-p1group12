// Package encapsulating database logic (reading, writing, updating, and deleting documents and their
// associated metadata)
package db

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"

	"github.com/RICE-COMP318-FALL24/owldb-p1group12/db/metadata"
	"github.com/RICE-COMP318-FALL24/owldb-p1group12/pair"
)

// Interface for a DBIndex; allows for upsert, remove, find, and query operations on the database
type DBIndex[K cmp.Ordered, V any] interface {
	Upsert(key K, check func(key K, currValue V, exists bool) (newValue V, err error)) (updated bool, err error)
	Remove(key K) (removedValue V, removed bool)
	Find(key K) (foundValue V, found bool)
	Query(ctx context.Context, start K, end K) (results []pair.Pair[K, V], err error)
}

// Database is a struct that exposes access/creation methods and the name of the DB
type Database struct {
	documents Collection
}

// Factory method for CollectionSets based on the passed in DBIndex implementation
type collectionSetFactory[K cmp.Ordered, V any] func() DBIndex[string, Collection]

// Factory method for Collections based on the passed in DBIndex implementation
type collectionFactory[K cmp.Ordered, V any] func() DBIndex[string, Document]

// Factory method to be used for creating a new collection in the database
var collectionMaker collectionFactory[string, Document]

// Factory method to be used for creating a new collection set in the database
var collectionSetMaker collectionSetFactory[string, Collection]

// Factory method for Databases; creates a new database with the given name
func New(name string, collectionFactory collectionFactory[string, Document], collectionSetFactory collectionSetFactory[string, Collection]) *Database {
	slog.Info("Creating database with name: ", name, "...")
	collectionSetMaker = collectionSetFactory
	collectionMaker = collectionFactory
	return &Database{
		documents: *NewCollection(),
	}
}

// Writes a document to the database with the given user, name, data, and overwrite flag
// Returns an error if the document already exists and overwrite is false
func (d *Database) WriteDocument(user string, path []string, data []byte, overwrite bool) (bool, error) {
	collection := d.documents
	var err error
	var document Document
	var found bool
	if len(path)%2 != 1 {
		return false, fmt.Errorf("invalid path; must end with a document")
	}
	// Traverse down to the location we want to insert the document
	for i := 0; i < len(path)-1; i++ {
		// If i is even, we are looking for a document; if i is odd, we are looking for a document
		if i%2 == 0 {
			document, found = collection.list.Find(path[i])
			if !found {
				return false, fmt.Errorf("containing document not found")
			}
		} else {
			collection, found = document.collections.Find(path[i])
			if !found {
				return false, fmt.Errorf("containing collection not found")
			}
		}
	}
	// Then, insert the document into the collection
	updated, err := collection.list.Upsert(path[len(path)-1], func(key string, currValue Document, exists bool) (Document, error) {
		if exists && !overwrite {
			return currValue, fmt.Errorf("Document already exists")
		}
		return NewDocument(user, data), nil
	})
	return updated, err
}

// Reads a document from the database with the given name, returning an error if it is not found
func (d *Database) ReadDocument(path []string) (Document, error) {
	// Iteratively traverse the path to get the document; if any part of the path is not found, return an error
	// Alternates between reading a document and a collection
	collection := d.documents
	var document Document
	var found bool
	if len(path)%2 == 0 {
		return Document{}, fmt.Errorf("invalid path; must end with a document")
	}
	// Note: this traverses all the way until the document is found, so the last element of the path must be a document
	for i := 0; i < len(path); i++ {
		// If i is even, we are looking for a document; if i is odd, we are looking for a document
		if i%2 == 0 {
			document, found = collection.list.Find(path[i])
			if !found {
				return Document{}, fmt.Errorf("containing document not found")
			}
		} else {
			collection, found = document.collections.Find(path[i])
			if !found {
				return Document{}, fmt.Errorf("containing collection not found")
			}
		}
	}
	slog.Debug("DB: Document found", "document", document)
	return document, nil
}

// Returns a map of document name : document for all the documents in a collection over a specified range
// Min, max are the start and end of the range; if BOTH min AND max are "", assume the range is not specified
func (d *Database) ReadDocuments(path []string, min string, max string, ctx context.Context) (map[string]Document, error) {
	// Iteratively traverse the path to get the document; if any part of the path is not found, return an error
	// Alternates between reading a document and a collection
	if len(path)%2 != 0 {
		return nil, fmt.Errorf("invalid path; must end with a collection")
	}
	var collection Collection = d.documents
	var err error
	var document Document
	var found bool
	// Note: this traverses all the way until the document is found, so the last element of the path must be a document
	for i := 0; i < len(path); i++ {
		// If i is even, we are looking for a document; if i is odd, we are looking for a document
		if i%2 == 0 {
			document, found = collection.list.Find(path[i])
			if !found {
				return nil, fmt.Errorf("containing document not found")
			}
		} else {
			collection, found = document.collections.Find(path[i])
			if !found {
				return nil, fmt.Errorf("containing collection not found")
			}
		}
	}
	sl, err := collection.list.Query(ctx, min, max)
	if err != nil {
		slog.Debug("DB: Error querying collection", "error", err)
		return nil, fmt.Errorf("error querying collection")
	} else {
		docMap := make(map[string]Document)
		for _, pair := range sl {
			docMap[pair.Key] = pair.Value
		}
		slog.Debug("DB: Documents found", "documents", docMap)
		return docMap, nil
	}
}

// Deletes a document from the database with the given name, returning an error if it is not found or if the path is incorrect
// Or if there is another error of some sort.
func (d *Database) DeleteDocument(path []string) error {
	collection := d.documents
	var document Document
	var found bool
	if len(path)%2 != 1 {
		return fmt.Errorf("invalid path; must end with a document")
	}
	// Note: this traverses all the way until the document is found, so the last element of the path must be a document
	for i := 0; i < len(path)-1; i++ {
		// If i is even, we are looking for a document; if i is odd, we are looking for a document
		if i%2 == 0 {
			document, found = collection.list.Find(path[i])
			if !found {
				slog.Debug("DB: Containing document not found")
				return fmt.Errorf("containing document not found")
			}
		} else {
			collection, found = document.collections.Find(path[i])
			if !found {
				slog.Debug("DB: Containing collection not found")
				return fmt.Errorf("containing collection not found")
			}
		}
	}
	_, removed := collection.list.Remove(path[len(path)-1])
	slog.Debug("DB: Document removed", "removed", removed)
	if !removed {
		slog.Debug("DB: Error in inner remove")
		return fmt.Errorf("error in inner remove")
	}
	return nil
}

// Deletes a collection from the database with the given name, returning an error if it is not found
func (d *Database) DeleteCollection(path []string) error {
	// Iteratively traverse the path to get the document; if any part of the path is not found, return an error
	// Alternates between reading a document and a collection
	if len(path)%2 != 0 {
		slog.Info("DB: Invalid path; must end with a collection")
		return fmt.Errorf("invalid path; must end with a collection")
	}
	var collection Collection = d.documents
	var document Document
	var found bool
	// Note: this traverses all the way until the document is found, so the last element of the path must be a document
	for i := 0; i < len(path)-1; i++ {
		// If i is even, we are looking for a document; if i is odd, we are looking for a document
		if i%2 == 0 {
			document, found = collection.list.Find(path[i])
			if !found {
				slog.Debug("DB: Containing document not found")
				return fmt.Errorf("containing document not found")
			}
		} else {
			collection, found = document.collections.Find(path[i])
			if !found {
				slog.Debug("DB: Containing collection not found")
				return fmt.Errorf("containing collection not found")
			}
		}
	}
	_, removed := document.collections.Remove(path[len(path)-1])
	if !removed {
		slog.Debug("DB: Error in inner remove")
		return fmt.Errorf("error in inner remove")
	} else {
		slog.Debug("DB: Collection removed")
		return nil
	}
}

// Method for creating a new collection in the database based on the collectionFactory passed in
func NewCollection() *Collection {
	return &Collection{
		list: collectionMaker(),
	}
}

// Struct encapsulating an implementation of the CollectionLike interface
type Collection struct {
	list DBIndex[string, Document]
}

// Creates a new collection with the given name in the document
func (d *Database) WriteCollection(path []string) error {
	if len(path)%2 != 0 {
		return fmt.Errorf("invalid path; must end with a collection")
	}
	if len(path) <= 1 {
		return fmt.Errorf("invalid path; must contain at least one containing document")
	}

	collection := d.documents
	var err error
	var document Document
	var found bool
	// Traverse down to the location we want to insert the collection
	for i := 0; i < len(path)-1; i++ {
		// If i is even, we are looking for a document; if i is odd, we are looking for a collection
		if i%2 == 0 {
			document, found = collection.list.Find(path[i])
			if !found {
				slog.Debug("DB: Containing document not found")
				return fmt.Errorf("containing document not found")
			}
		} else {
			collection, found = document.collections.Find(path[i])
			if !found {
				slog.Debug("DB: Containing collection not found")
				return fmt.Errorf("containing collection not found")
			}
		}
	}

	// Insert the collection into the document
	var collName = path[len(path)-1]
	_, err = document.collections.Upsert(collName, func(key string, currValue Collection, exists bool) (Collection, error) {
		if exists {
			slog.Debug("DB: Collection already exists")
			return currValue, fmt.Errorf("Collection already exists")
		}
		slog.Debug("DB: Collection created")
		return *NewCollection(), nil
	})

	return err
}

// Reads a collection from the database with the given name, returning an error if it is not found
// returns the default document if the path is empty
func (c *Collection) ReadDocument(path []string) (Document, error) {
	if len(path) != 1 {
		return Document{}, fmt.Errorf("invalid path")
	}
	doc, found := c.list.Find(path[0])
	if found {
		return doc, nil
	} else {
		return Document{}, fmt.Errorf("Document not found")
	}
}

// Returns a map of document name : document for all the documents in a collection over a specified range
func (c *Collection) ReadDocuments(path []string, min string, max string, ctx context.Context) (map[string]Document, error) {
	if len(path) != 0 {
		return nil, fmt.Errorf("invalid path")
	}
	sl, err := c.list.Query(ctx, min, max)
	docMap := make(map[string]Document)
	if err != nil {
		for _, pair := range sl {
			docMap[pair.Key] = pair.Value
		}
		return docMap, nil
	}
	return nil, fmt.Errorf("error querying collection")
}

// Struct encapsulating an implementation of the Document interface
type Document struct {
	metadata    metadata.Metadata
	data        []byte
	collections DBIndex[string, Collection]
}

// Interface for a CollectionLike; allows for reading documents and collections
type CollectionLike interface {
	ReadDocument(path []string) (Document, error)
	ReadDocuments(path []string, min string, max string, ctx context.Context) (map[string]Document, error)
}

// Returns a copy of the collections
func (d Document) GetDocCollections() DBIndex[string, Collection] {
	slog.Info("Getting document collections...")
	return d.collections
}

// Returns a copy of the data
func (d Document) GetDocData() []byte {
	slog.Info("Getting document data...")
	return d.data
}

// Returns a copy of the metadata (in as Metadata struct)
func (d Document) GetDocMetadata() metadata.Metadata {
	slog.Info("Getting document metadata...")
	return d.metadata
}

// Creator method for Documents; creates a new document with the given user and data with
// appropriate metadata based on the collectionSetFactory passed in
func NewDocument(user string, data []byte) Document {
	slog.Info("Creating new document...")
	return Document{
		metadata:    metadata.New(user),
		data:        data,
		collections: collectionSetMaker(),
	}
}
