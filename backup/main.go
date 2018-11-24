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

func exportAll(base, path string) error {
	fmt.Printf("exporting all from %q", base+path)
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

	fw := tar.NewWriter(os.Stdout)
	defer fw.Close()
	for _, v := range files {
		if err = export1(base, v, fw); err != nil {
			return err
		}
	}

	return nil
}

func importAll(base string) error {
	fr := tar.NewReader(os.Stdin)
	for {
		h, err := fr.Next()
		if err == io.EOF {
			return nil
		}
		req, err := http.NewRequest(http.MethodPut, base+h.Name, fr)
		if err != nil {
			return err
		}
		if mn := h.Xattrs["user.fs_module_type"]; mn != "" {
			req.Header.Add("X-FS-Module-Type", mn)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
	}
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
		return fmt.Errorf("getting %q: %v", base+path, err)
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
	backend  = flag.String("backend", "", "")
	modeFlag = flag.String("mode", "", "")
)

func main() {
	flag.Parse()
	var err error
	switch *modeFlag {
	case "export":
		err = exportAll(*backend, flag.Arg(0))
	case "import":
		err = importAll(*backend)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s", err)
	}
}
