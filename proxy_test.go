package gproxy_test

import (
	"context"
	"fmt"
	"github.com/graphikDB/gproxy"
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

	proxy, err := gproxy.New(ctx,
		gproxy.WithInsecurePort(8080),
		gproxy.WithHTTPRoutes(func(ctx context.Context, host string) string {
			return srv.URL
		}),
		gproxy.WithHostPolicy(func(ctx context.Context, host string) error {
			return nil
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
	proxy, err := gproxy.New(ctx,
		gproxy.WithInsecurePort(8080),
		gproxy.WithHTTPRoutes(func(ctx context.Context, host string) string {
			return srv.URL
		}),
		gproxy.WithHostPolicy(func(ctx context.Context, host string) error {
			return nil
		}))
	if err != nil {
		t.Fatal(err.Error())
	}
	go func() {
		if err := proxy.Serve(ctx); err != nil {
			t.Fatal(err.Error())
		}
	}()

	resp, err := http.DefaultClient.Get("http://localhost:8080/")
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
	srv.Close()

}
