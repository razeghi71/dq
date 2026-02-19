package main

import (
	"log"
	"os"

	parquet "github.com/parquet-go/parquet-go"
)

type User struct {
	Name string `parquet:"name"`
	Age  int32  `parquet:"age"`
	City string `parquet:"city"`
}

func main() {
	f, err := os.Create("testdata/users.parquet")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	w := parquet.NewWriter(f)

	users := []User{
		{"Alice", 30, "NY"},
		{"Bob", 25, "LA"},
		{"Charlie", 35, "NY"},
		{"Diana", 28, "SF"},
		{"Eve", 22, "LA"},
		{"Frank", 40, "NY"},
	}

	for _, u := range users {
		if err := w.Write(u); err != nil {
			log.Fatal(err)
		}
	}

	if err := w.Close(); err != nil {
		log.Fatal(err)
	}
}
