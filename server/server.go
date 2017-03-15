package main

import (
	"crypto/sha1"
	"fmt"
	//	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("Usage: %s <port>", os.Args[0])
	}
	if _, err := strconv.Atoi(os.Args[1]); err != nil {
		log.Fatalf("Invalid port: %s (%s)\n", os.Args[1], err)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		dump, err := httputil.DumpRequest(req, false)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Fprintf(w, "%s", dump)
		body, _ := ioutil.ReadAll(req.Body)
		s := sha1.Sum(body)

		fmt.Printf("---> %s %s %s %s sha1:%x\n", os.Args[1], req.Method, req.URL.String(), req.UserAgent(), s)
		fmt.Fprintf(w, "Body(sha1): %x\n", s)
	})
	http.ListenAndServe(":"+os.Args[1], nil)
}
