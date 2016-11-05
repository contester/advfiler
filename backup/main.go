package main

import (
	"bufio"
	"flag"
	"fmt"
	"net/http"
)

func exportAll(base, path string) error {
	resp, err := http.Get(base + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var files []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		files = append(files, scanner.Text())
	}
	if err = scanner.Err(); err != nil {
		return err
	}
	fmt.Println(len(files))
	return nil
}

var (
	backend = flag.String("backend", "", "")
)

func main() {
	flag.Parse()
	fmt.Println(exportAll(*backend, flag.Arg(0)))
}
