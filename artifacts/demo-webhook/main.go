package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"net/http"


)

var port = "5001"

func init() {
	flag.StringVar(&port, "p", "5001", "port")
	flag.Parse()
}

func main() {
	log.Println("starting....")

	log.Fatal(http.ListenAndServe(":"+port, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			panic(err)
		}
		defer r.Body.Close()
		var buf bytes.Buffer
		if err := json.Indent(&buf, b, " >", "  "); err != nil {
			panic(err)
		}
		log.Println(buf.String())
	})))

}
