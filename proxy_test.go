package gproxy_test

import (
	"context"
	"fmt"
	"github.com/graphikDB/gproxy"
	"github.com/graphikDB/gproxy/logger"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func ExampleNew() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello world"))
	}))
	defer srv.Close()
	proxy, err := gproxy.New(ctx,
		// serve unencrypted http/gRPC traffic on port 8080
		gproxy.WithInsecurePort(8080),
		// serve encrypted http/gRPC traffic on port 443
		gproxy.WithSecurePort(443),
		// if the request is http & the request host contains localhost, proxy to the target server
		gproxy.WithTrigger(fmt.Sprintf(`this.http && this.host.contains('localhost') => '%s'`, srv.URL)),
		// when deploying, set the letsencrypt allowed domains
		gproxy.WithLetsEncryptHosts([]string{
			// "www.graphikdb.io",
		}))
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	if err := proxy.Serve(ctx); err != nil {
		fmt.Println(err.Error())
		return
	}
	// Output:
}

func Test(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello world"))
	}))
	defer srv.Close()
	proxy, err := gproxy.New(ctx,
		gproxy.WithInsecurePort(8081),
		gproxy.WithSecurePort(8082),
		gproxy.WithLogger(logger.New(true)),
		gproxy.WithTrigger(
			fmt.Sprintf(
				`this.http && this.host.contains("localhost") => "%s"`,
				srv.URL,
			)),
		// when deploying, set the letsencrypt allowed domains
		gproxy.WithLetsEncryptHosts([]string{
			// "www.graphikdb.io",
		}))
	if err != nil {
		t.Fatal(err.Error())
	}
	go func() {
		if err := proxy.Serve(ctx); err != nil {
			t.Fatal(err.Error())
		}
	}()
	time.Sleep(2 * time.Second)
	resp, err := http.DefaultClient.Get("http://localhost:8081/")
	if err != nil {
		t.Fatal(err.Error())
	}
	defer resp.Body.Close()
	bits, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err.Error())
	}
	if string(bits) != "hello world" {
		t.Fatal("failed to proxy in mem request")
	}
	cancel()

}
