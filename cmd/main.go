package main

import (
	"log"
	"os"

	"github.com/husainaloos/hzip"
)

func main() {
	f, err := os.Open("../test/rfc1952.txt.gz")
	if err != nil {
		log.Fatalf("cannot read file: %v", err)
	}
	defer f.Close()
	rb, err := hzip.NewReaderBuilder(f)
	if err != nil {
		log.Fatal(err)
	}

	_, err = rb.Reader()
	if err != nil {
		log.Fatal(err)
	}

}
