package main

import (
	"fmt"
	"net/http"
	"io"
	"net/url"
)

func main() {
	query := "Mushoku Tensei Season 3"
	url := "https://senshi.tv/api/v2/search?query=" + url.QueryEscape(query)
	resp, _ := http.Get(url)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	fmt.Println(string(b))
}
