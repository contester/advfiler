package main

import (
	"archive/tar"
	"bufio"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
)

func exportAll(base, path, archive string) error {
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

	f, err := os.Create(archive)
	if err != nil {
		return err
	}
	defer f.Close()

	fw := tar.NewWriter(f)
	defer fw.Close()
	for _, v := range files {
		if err = export1(base, v, fw); err != nil {
			return err
		}
	}

	return nil
}

func export1(base, path string, fw *tar.Writer) error {
	resp, err := http.Get(base + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	sz := resp.Header.Get("X-Fs-Content-Length")
	isz, err := strconv.ParseInt(sz, 10, 64)
	if err != nil {
		return err
	}
	fh := tar.Header{
		Name:     path,
		Mode:     0666,
		Size:     isz,
		Typeflag: tar.TypeReg,
	}
	if mn := resp.Header.Get("X-Fs-Module-Type"); mn != "" {
		//fh.Name = fh.Name + "." + mn
		fh.Xattrs = map[string]string{"user.fs_module_type": mn}
	}
	if err = fw.WriteHeader(&fh); err != nil {
		return err
	}
	_, err = io.Copy(fw, resp.Body)
	return err
}

var (
	backend = flag.String("backend", "", "")
	arcp    = flag.String("archive", "", "")
)

func main() {
	flag.Parse()
	fmt.Println(exportAll(*backend, flag.Arg(0), *arcp))
}
