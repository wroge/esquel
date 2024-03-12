[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white)](https://pkg.go.dev/github.com/wroge/esquel)
[![GitHub tag (latest SemVer)](https://img.shields.io/github/tag/wroge/esquel.svg?style=social)](https://github.com/wroge/esquel/tags)

# esquel

A package for creating SQL statements and scanning rows into Go structs without reflection.

```go
go get github.com/wroge/esquel
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
		Statement: esquel.Stmt[string]("INSERT INTO authors (name) VALUES (?) RETURNING id"),
	}

	ammousID, err := insertAuthor.One(ctx, db, "Saifedean Ammous")
	if err != nil {
		panic(err)
	}
	// INSERT INTO authors (name) VALUES (?) RETURNING id
	// [Saifedean Ammous]

	insertAuthors := esquel.Query[int64, []string]{
		Statement: esquel.Stmt("INSERT INTO authors (name) VALUES ? RETURNING id",
			esquel.List(esquel.Stmt[string]("(?)")),
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
		Statement: esquel.Stmt("INSERT INTO books (title, author_id) VALUES ? RETURNING id",
			esquel.List(esquel.Values(func(param InsertBook) []any {
				return []any{param.Title, param.AuthorID}
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
		Statement: esquel.Stmt(`
			SELECT books.id AS book_id, books.title AS book_title, books.created AS book_created, 
				authors.id AS author_id, authors.name AS author_name
			FROM books LEFT JOIN authors ON authors.id = books.author_id ? LIMIT 10`,
			esquel.Prefix("WHERE", esquel.Join(" AND ",
				esquel.Expr(func(q QueryBook) (string, []any, error) {
					if q.Title == "" {
						return "", nil, nil
					}

					return "books.title = ?", []any{q.Title}, nil
				}),
				esquel.Expr(func(q QueryBook) (string, []any, error) {
					if q.Author == "" {
						return "", nil, nil
					}

					return "authors.name = ?", []any{q.Author}, nil
				}),
			))),
		Columns: map[string]esquel.Scanner[Book]{
			"book_id":    esquel.Scan(func(b *Book, id int64) { b.ID = id }),
			"book_title": esquel.Scan(func(b *Book, title sql.NullString) { b.Title = title.String }),
			"book_created": esquel.Scan(func(b *Book, created time.Time) { b.Created = created }).
				AsString(func(s string) (time.Time, error) {
					return time.Parse(time.DateTime, s)
				}),
			"author_id":   esquel.Scan(func(b *Book, id int64) { b.Author.ID = id }),
			"author_name": esquel.Scan(func(b *Book, name string) { b.Author.Name = name }),
		},
	}

	books, err := queryBooks.All(ctx, db, QueryBook{Title: "The Bitcoin Standard"})
	if err != nil {
		panic(err)
	}
	// SELECT books.id AS book_id, books.title AS book_title, books.created AS book_created,
	// 	authors.id AS author_id, authors.name AS author_name
	// FROM books LEFT JOIN authors ON authors.id = books.author_id WHERE books.title = ? LIMIT 10
	// [The Bitcoin Standard]

	fmt.Println(books)
	// [{1 The Bitcoin Standard {1 Saifedean Ammous} 2024-03-08 13:10:36 +0000 UTC}]

	books, err = queryBooks.All(ctx, db, QueryBook{Author: "Vijay Boyapati", Title: "The Bullish Case for Bitcoin"})
	if err != nil {
		panic(err)
	}
	// SELECT
	// 	books.id AS book_id, books.title AS book_title, books.created AS book_created,
	// 	authors.id AS author_id, authors.name AS author_name
	// FROM books LEFT JOIN authors ON authors.id = books.author_id
	// WHERE books.title = ? AND authors.name = ?
	// [The Bullish Case for Bitcoin Vijay Boyapati]

	fmt.Println(books)
	// [{3 The Bullish Case for Bitcoin {3 Vijay Boyapati} 2024-03-08 13:10:36 +0000 UTC}]
}
```