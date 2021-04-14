package bililive

import (
	"crypto/tls"
	"io/ioutil"
	"net/http"
	"net/url"
)

func httpSend(url string, proxy func(*http.Request) (*url.URL, error)) ([]byte, error) {
	tr := &http.Transport{ //解决x509: certificate signed by unknown authority
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		Proxy:           proxy,
	}
	client := &http.Client{Transport: tr}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func defaultProxy() func(*http.Request) (*url.URL, error) {
	return nil
}
