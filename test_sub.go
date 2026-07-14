package main

import (
	"fmt"
	"net/url"
)

func main() {
	u, _ := url.Parse("https://bysesayeveum.com/e/utq75rkvwt20/?sub.info=https%3A%2F%2Fninstream.com%2FadJAZFRTe9LTmwJ4j3X9wA%2F1784064626%2Fabcee2f8-57f7-485a-b9ac-f4a8b220361c%2Fsub_filemoon.json")
	fmt.Println(u.Query().Get("sub.info"))
}
