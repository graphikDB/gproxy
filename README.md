![graphik](https://github.com/graphikDB/assets/blob/master/branding/graphik-logo.jpg?raw=true)

gproxy is a reverse proxy service AND library for creating flexible, [expression-based]((github.com/graphikDB/trigger)), [lets-encrypt/acme]((https://letsencrypt.org/)) secured gRPC/http reverse proxies    
  
# GProxy as a Library

Library Documentation: [![GoDoc](https://godoc.org/github.com/graphikDB/gproxy?status.svg)](https://godoc.org/github.com/graphikDB/gproxy)


    go get -u github.com/graphikDB/gproxy


- [x] Automatic [LetsEncrypt/Acme](https://letsencrypt.org/) Based SSL Encryption
- [x] Transparent gRPC Proxy(including streaming)
- [x] Transparent http Proxy(including websockets)
- [x] [Expression-Based](github.com/graphikDB/trigger) Routing
- [x] [Expression-Based](github.com/graphikDB/trigger) Acme Host Policies
- [x] Functional Arguments for extensive configuration of http(s) & grpc servers
- [x] Graceful Shutdown

```go
        proxy, err := gproxy.New(ctx,
		// serve unencrypted http/gRPC traffic on port 8080
		gproxy.WithInsecurePort(8080),
		// serve encrypted http/gRPC traffic on port 443
		gproxy.WithSecurePort(443),
		// if the request is http & the request host contains localhost, proxy to the target http server
		gproxy.WithRoute(fmt.Sprintf(`this.http && this.host.endsWith('graphikdb.io') => "%s"`, httpServer.URL)),
        // if the request is gRPC & the request host contains localhost, proxy to the target gRPC server
		gproxy.WithRoute(fmt.Sprintf(`this.grpc && this.host.endsWith('graphikdb.io') => "%s"`, grpcServer.URL)),
		// when deploying, set the letsencrypt list of allowed domains
		gproxy.WithAcmePolicy("this.host.contains('graphikdb.io')"))
	if err != nil {
		fmt.Println(err.Error())
		return
	}
    // start blocking server
	if err := proxy.Serve(ctx); err != nil {
		fmt.Println(err.Error())
		return
	}
```

# GProxy as a Service

    docker pull graphikDB:gproxy:v0.0.17

- [x] Automatic [LetsEncrypt/Acme](https://letsencrypt.org/) Based SSL Encryption
- [x] Transparent gRPC Proxy(including streaming)
- [x] Transparent http Proxy(including websockets)
- [x] Graceful Shutdown
- [x] CORS
- [x] [Expression-Based](github.com/graphikDB/trigger) Acme Host Policies
- [x] [Expression-Based](github.com/graphikDB/trigger) Routing
- [x] 12-Factor Config
- [x] Hot Reload Config
- [x] Dockerized(graphikDB:gproxy:v0.0.17)
- [x] K8s Deployment Manifest
    
default config path: ./gproxy.yaml which may be changed with the --config flag or the GRAPHIK_CONFIG environmental variable

Example Config:

```yaml
debug: true
autocert:
  policy: "this.host.contains('graphikdb.io')"
routing:
  - "this.http && this.host.endsWith('graphikdb.io') => 'http://localhost:7821'"
  - "this.grpc && this.host.endsWith('graphikdb.io') => 'localhost:7820'"
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
watch: true # hot reload config changes
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
      policy: "this.host.contains('graphikdb.io')"
    routing:
      - "this.http && this.host.endsWith('graphikdb.io') => { 'target': 'http://localhost:7821' }"
      - "this.grpc && this.host.endsWith('graphikdb.io') => { 'target': 'localhost:7820' }"
    server:
      insecure_port: 80
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
    watch: true # hot reload config changes
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
          image: graphikdb/gproxy:v0.0.17
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

save to k8s.yaml & apply with 

    kubectl apply -f k8s.yaml
    
    
watch as pods come up:

    kubectl get pods -n gproxy -w
    

check LoadBalancer ip:

    kubectl get svc -n gproxy
