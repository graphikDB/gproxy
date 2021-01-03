![graphik](assets/graphik-logo.jpg)

gproxy is a reverse proxy service AND library for creating flexible, expression-based, lets-encrypt secured gRPC and http reverse proxies

[![GoDoc](https://godoc.org/github.com/graphikDB/gproxy?status.svg)](https://godoc.org/github.com/graphikDB/gproxy)
[trigger/expression language reference]("https://github.com/graphikdb/trigger")

    go get -u github.com/graphikDB/gproxy
    
    docker pull graphikDB:gproxy:v0.0.7
    
    
```go
    proxy, err := gproxy.New(ctx,
		// serve unencrypted http/gRPC traffic on port 8080
		gproxy.WithInsecurePort(8080),
		// serve encrypted http/gRPC traffic on port 443
		gproxy.WithSecurePort(443),
		// if the request is http & the request host contains localhost, proxy to the target server
		gproxy.WithTrigger(fmt.Sprintf(`this.http && this.host.contains('localhost') => { "target": "%s"}`, srv.URL)), // must return "target" attribute in the json map
		// when deploying, set the letsencrypt list of allowed domains
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
```

## Features

- [x] Use as Library
- [x] Use as Service
- [x] Automatic Lets Encrypt Based SSL Encryption
- [x] Transparent gRPC Proxy(including streaming)
- [x] Transparent http Proxy(including websockets)
- [x] Expression-Based Routing
- [x] K8s Deployment Manifest
- [x] 12-Factor Config
- [ ] Hot Reload Config

# GProxy as a Service
    
default config path: ./gproxy.yaml which may be changed with the --config flag or the GRAPHIK_CONFIG environmental variable

Example Config:

```yaml
debug: true
autocert:
  - "www.example.com"
routing:
  - "this.http && this.host == 'localhost:8080' => { 'target': 'http://localhost:7821' }"
  - "this.grpc && this.host == 'localhost:8080' => { 'target': 'localhost:7820' }"
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
      - "this.http && this.host == 'localhost:8080' => { 'target': 'http://localhost:7821' }"
      - "this.grpc && this.host == 'localhost:8080' => { 'target': 'localhost:7820' }"
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
          image: graphikdb/gproxy:v0.0.7
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

### Docker-Compose

**Coming Soon**