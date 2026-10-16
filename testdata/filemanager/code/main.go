package main

import (
	"fmt"
	"net/http"
)

// Fetch a config file served by file_manager.
func main() {
	resp, err := http.Get("http://localhost:8080/files/raw/configs/prod/gateway.json")
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	fmt.Println(resp.Status, resp.Header.Get("Content-Type"))
}
