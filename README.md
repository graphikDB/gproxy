# gproxy

a library for creating lets-encrypt secured gRPC and http reverse proxies 

[![GoDoc](https://godoc.org/github.com/graphikDB/gproxy?status.svg)](https://godoc.org/github.com/graphikDB/gproxy)

    
    go get -u github.com/graphikDB/gproxy
    
    
    
```go
        proxy, err := gproxy.New(ctx,
		gproxy.WithInsecurePort(8080),
		gproxy.WithHTTPRoutes(func(ctx context.Context, host string) string {
			if host == "acme.graphik.com" {
				return "graphik.acme.cluster.local"
			}
			return "" //
		}),
		gproxy.WithHostPolicy(func(ctx context.Context, host string) error {
			if host != "www.graphik.io" {
                return errors.New("forbidden")
            }       
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
```

# GProxy Service

    docker pull graphikDB:gproxy:v0.0.2
    
default config path: gproxy.yaml

## Example Config

```yaml
debug: true
autocert:
  - "www.example.com"
routing:
  http:
    - "this.host == 'localhost:8080' => { 'target': 'http://localhost:7821' }"
server:
  insecure_port: 8080
  secure_port: 443
cors:
  origins: "*"
  methods: "*"
  headers:
    - "GET"
    - "POST"
    - "PUT"
    - "DELETE"
    - "PATCH"
```