# esquel

A package for creating SQL statements and scanning rows into Go structs without reflection. You can pronounce it however you like, but my choice is *es'kel*.

```go
go get github.com/wroge/esquel

import "github.com/wroge/esquel"
import eskel "github.com/wroge/esquel"
import sequel "github.com/wroge/esquel"
import squeal "github.com/wroge/esquel"
```

## Example

```go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/wroge/esquel"

	_ "github.com/mattn/go-sqlite3"
)

type Author struct {
	ID   int64
	Name string
}

type Book struct {
	ID      int64
	Title   string
	Author  Author
	Created time.Time
}

type InsertBook struct {
	Title    string
	AuthorID int64
}

type QueryBook struct {
	Author string
	Title  string
}

func main() {
	ctx := context.Background()

	db, err := sql.Open("sqlite3", "file:test.db?cache=shared&mode=memory")
	if err != nil {
		panic(err)
	}

	_, err = db.Exec(`
	CREATE TABLE authors (
		id INTEGER PRIMARY KEY, 
		name TEXT NOT NULL
	)`)
	if err != nil {
		panic(err)
	}

	_, err = db.Exec(`
	CREATE TABLE books (
		id INTEGER PRIMARY KEY, 
		title TEXT, 
		author_id INTEGER REFERENCES authors(id), 
		created DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		panic(err)
	}

	insertAuthor := esquel.Query[int64, string]{
		Statement: esquel.Template[string]("INSERT INTO authors (name) VALUES (?) RETURNING id"),
	}

	ammousID, err := insertAuthor.One(ctx, db, "Saifedean Ammous")
	if err != nil {
		panic(err)
	}
	// INSERT INTO authors (name) VALUES (?) RETURNING id
	// [Saifedean Ammous]

	insertAuthors := esquel.Query[int64, []string]{
		Statement: esquel.Template("INSERT INTO authors (name) VALUES ? RETURNING id",
			esquel.List(",", esquel.Template[string]("(?)")),
		),
	}

	authorsID, err := insertAuthors.All(ctx, db, []string{"Andreas M. Antonopoulos", "Vijay Boyapati"})
	if err != nil {
		panic(err)
	}
	// INSERT INTO authors (name) VALUES (?),(?) RETURNING id
	// [Andreas M. Antonopoulos Vijay Boyapati]

	antonopoulosID, boyapatiID := authorsID[0], authorsID[1]

	insertBooks := esquel.Query[int64, []InsertBook]{
		Statement: esquel.Template("INSERT INTO books (title, author_id) VALUES ? RETURNING id",
			esquel.List(",", esquel.Func(func(param InsertBook) (string, []any, error) {
				return "(?,?)", []any{param.Title, param.AuthorID}, nil
			})),
		),
	}

	bookIDs, err := insertBooks.All(ctx, db, []InsertBook{
		{AuthorID: ammousID, Title: "The Bitcoin Standard"},
		{AuthorID: antonopoulosID, Title: "The Internet of Money"},
		{AuthorID: boyapatiID, Title: "The Bullish Case for Bitcoin"},
	})
	if err != nil {
		panic(err)
	}
	// INSERT INTO books (title, author_id) VALUES (?,?),(?,?),(?,?) RETURNING id
	// [The Bitcoin Standard 1 The Internet of Money 2 The Bullish Case for Bitcoin 3]

	fmt.Println(bookIDs)
	// [1 2 3]

	queryBooks := esquel.Query[Book, QueryBook]{
		Statement: esquel.Template(`
SELECT 
	books.id AS book_id, books.title AS book_title, books.created AS book_created, 
	authors.id AS author_id, authors.name AS author_name
FROM books LEFT JOIN authors ON authors.id = books.author_id
`,
			esquel.Prefix("WHERE", esquel.Join(" AND ",
				esquel.Func(func(q QueryBook) (string, []any, error) {
					if q.Title == "" {
						return "", nil, nil
					}

					return "books.title = ?", []any{q.Title}, nil
				}),
				esquel.Func(func(q QueryBook) (string, []any, error) {
					if q.Author == "" {
						return "", nil, nil
					}

					return "authors.name = ?", []any{q.Author}, nil
				}),
			)),
		),
		Columns: map[string]esquel.Scanner[Book]{
			"book_id":    esquel.Scan(func(b *Book, id int64) { b.ID = id }),
			"book_title": esquel.ScanNull("No Title", func(b *Book, title string) { b.Title = title }),
			"book_created": func() (any, func(*Book) error) {
				var created string

				return &created, func(b *Book) error {
					var err error

					b.Created, err = time.Parse(time.DateTime, created)

					return err
				}
			},
			"author_id":   esquel.Scan(func(b *Book, id int64) { b.Author.ID = id }),
			"author_name": esquel.Scan(func(b *Book, name string) { b.Author.Name = name }),
		},
	}

	books, err := queryBooks.All(ctx, db, QueryBook{Title: "The Bitcoin Standard"})
	if err != nil {
		panic(err)
	}
	// SELECT
	// 	books.id AS book_id, books.title AS book_title, books.created AS book_created,
	// 	authors.id AS author_id, authors.name AS author_name
	// FROM books LEFT JOIN authors ON authors.id = books.author_id WHERE books.title = ?
	// [The Bitcoin Standard]

	fmt.Println(books)
	// [{1 The Bitcoin Standard {1 Saifedean Ammous} 2024-03-08 13:10:36 +0000 UTC}]

	books, err = queryBooks.All(ctx, db, QueryBook{Author: "Vijay Boyapati"})
	if err != nil {
		panic(err)
	}
	// SELECT
	// 	books.id AS book_id, books.title AS book_title, books.created AS book_created,
	// 	authors.id AS author_id, authors.name AS author_name
	// FROM books LEFT JOIN authors ON authors.id = books.author_id WHERE books.title = ? AND authors.name = ?
	// [The Bullish Case for Bitcoin Vijay Boyapati]

	fmt.Println(books)
	// [{3 The Bullish Case for Bitcoin {3 Vijay Boyapati} 2024-03-08 13:10:36 +0000 UTC}]
}
```