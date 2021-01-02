package gproxy

import (
	"fmt"
	"github.com/graphikDB/gproxy/logger"
	"golang.org/x/crypto/acme/autocert"
	"google.golang.org/grpc"
)

// Opt is a function that configures a Proxy instance
type Opt func(p *Proxy)

// WithHostPolicy sets the host policy function on the  proxy(required)
func WithHostPolicy(policy autocert.HostPolicy) Opt {
	return func(p *Proxy) {
		p.hostPolicy = policy
	}
}

// WithLogger sets the proxies logger instance(optional)
func WithLogger(logger *logger.Logger) Opt {
	return func(p *Proxy) {
		p.logger = logger
	}
}

// WithMiddlewares sets the http middlewares on encrypted & non-encrypted traffic(optional)
func WithMiddlewares(middlewares ...Middleware) Opt {
	return func(p *Proxy) {
		p.middlewares = append(p.middlewares, middlewares...)
	}
}

// WithGRPCRoutes sets the gRPC RouterFunc which takes a hostname and returns an endpoint to route to.
// either http routes, gRPC routes, or both are required.
func WithGRPCRoutes(router RouterFunc) Opt {
	return func(p *Proxy) {
		p.gRPCRouter = router
	}
}

// WithHTTPRoutes sets the http RouterFunc which takes a hostname and returns an endpoint to route to
// either http routes, gRPC routes, or both are required.
func WithHTTPRoutes(router RouterFunc) Opt {
	return func(p *Proxy) {
		p.httpRouter = router
	}
}

// WithInsecurePort sets the port that non-encrypted traffic will be served on(optional)
func WithInsecurePort(insecurePort int) Opt {
	return func(p *Proxy) {
		p.insecurePort = fmt.Sprintf(":%v", insecurePort)
	}
}

// WithSecurePort sets the port that encrypted traffic will be served on(optional)
func WithSecurePort(securePort int) Opt {
	return func(p *Proxy) {
		p.securePort = fmt.Sprintf(":%v", securePort)
	}
}

// WithUnaryInterceptors adds gRPC unary interceptors to the proxy instance
func WithUnaryInterceptors(uinterceptors ...grpc.UnaryServerInterceptor) Opt {
	return func(p *Proxy) {
		p.uinterceptors = append(p.uinterceptors, uinterceptors...)
	}
}

// WithStreamInterceptors adds gRPC stream interceptors to the proxy instance
func WithStreamInterceptors(sinterceptors ...grpc.StreamServerInterceptor) Opt {
	return func(p *Proxy) {
		p.sinterceptors = append(p.sinterceptors, sinterceptors...)
	}
}

// WithCertCacheDir sets the directory in which certificates will be cached (default: /tmp/certs)
func WithCertCacheDir(certCache string) Opt {
	return func(p *Proxy) {
		p.certCache = certCache
	}
}
