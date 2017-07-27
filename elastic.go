package gindex

import (
	"net/http"
	"fmt"
	"bytes"
)

type ElServer struct {
	adress string
	port   int
}

func (el *ElServer) Index(index, doctype string, data []byte) (*http.Response, error) {
	adrr := fmt.Sprintf("%s:%d/%s/%s", el.adress, el.port, index, doctype)
	req, err := http.NewRequest("PUT", adrr, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	cl := http.Client{}
	return cl.Do(req)

}
