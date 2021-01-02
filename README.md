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

# GProxy as a Service

    docker pull graphikDB:gproxy:v0.0.6
    
default config path: ./gproxy.yaml which may be changed with the --config flag or the GRAPHIK_CONFIG environmental variable

Example Config:

```yaml
# enable debug logs
debug: true
# lets encrypt autocert allowed domains
autocert:
  - "www.example.com"
routing:
  # http reverse proxy routes using trigger framework: github.com/graphikDB/trigger
  http:
    - "this.host == 'localhost:8080' => { 'target': 'http://localhost:7821' }"
  grpc:
    - "this.host == 'localhost:8080' => { 'target': 'localhost:7820' }"
server:
  # unencrypted server port
  insecure_port: 8080
  # encrypted server port
  secure_port: 443
# cross origin resource sharing config
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

## Deployment

### Kubernetes

example manifest:
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: gproxy
---
kind: ConfigMap
apiVersion: v1
metadata:
  name: gproxy-config
  namespace: gproxy
data:
  gproxy.yaml: |-
    debug: true
    autocert:
      - "www.example.com"
    routing:
      http:
        - "this.host == 'localhost:8080' => { 'target': 'http://localhost:7821' }"
      grpc:
        - "this.host == 'localhost:8080' => { 'target': 'localhost:7820' }"
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
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: gproxy
  namespace: gproxy
  labels:
    app: gproxy
spec:
  replicas: 1
  selector:
    matchLabels:
      app: gproxy
  serviceName: "gproxy"
  template:
    metadata:
      labels:
        app: gproxy
    spec:
      containers:
        - name: gproxy
          image: graphikdb/gproxy:v0.0.6
          imagePullPolicy: Always
          ports:
            - containerPort: 80
            - containerPort: 443
          env:
            - name: GPROXY_CONFIG
              value: /tmp/gproxy/gproxy.yaml
          volumeMounts:
            - mountPath: /tmp/certs
              name: certs-volume
            - mountPath: /tmp/gproxy/gproxy.yaml
              name: config-volume
              subPath: gproxy.yaml
      volumes:
        - name: config-volume
          configMap:
            name: gproxy-config
  volumeClaimTemplates:
    - metadata:
        name: certs-volume
      spec:
        accessModes: [ "ReadWriteOnce" ]
        resources:
          requests:
            storage: 5Mi

---
apiVersion: v1
kind: Service
metadata:
  name: gproxy
  namespace: gproxy
spec:
  selector:
    app: gproxy
  ports:
    - protocol: TCP
      port: 80
      name: insecure
    - protocol: TCP
      port: 443
      name: secure
  type: LoadBalancer
---

```

apply with `kubectl apply -f k8s.yaml`