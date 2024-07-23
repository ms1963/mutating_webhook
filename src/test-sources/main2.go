package main

import (
  "net/http"
	"log"
)

func main() {
  http.HandleFunc("/", HandleRoot)
	http.HandleFunc("/mutate", HandleMutate)
  log.Fatal(http.ListenAndServe(":80", nil))
}

func HandleRoot(w http.ResponseWriter, r *http.Request){
	w.Write([]byte("reached /"))
}

func HandleMutate(w http.ResponseWriter, r *http.Request){
	w.Write([]byte("reached /mutate!"))
}
