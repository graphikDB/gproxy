package gproxy

import (
	"context"
	"fmt"
	"github.com/graphikDB/gproxy/logger"
	"github.com/graphikDB/trigger"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"net/http"
)

// Opt is a function that configures a Proxy instance
type Opt func(p *Proxy) error

// WithLetsEncryptHosts sets the letsencryp host policy on the proxy(required)
func WithLetsEncryptHosts(allowedHosts []string) Opt {
	return func(p *Proxy) error {
		p.hostPolicy = func(ctx context.Context, host string) error {
			for _, h := range allowedHosts {
				if h == host {
					return nil
				}
			}
			return errors.Errorf("forbidden host: %s", host)
		}
		return nil
	}
}

// WithLogger sets the proxies logger instance(optional)
func WithLogger(logger *logger.Logger) Opt {
	return func(p *Proxy) error {
		p.logger = logger
		return nil
	}
}

// WithMiddlewares sets the http middlewares on encrypted & non-encrypted traffic(optional)
func WithMiddlewares(middlewares ...Middleware) Opt {
	return func(p *Proxy) error {
		p.middlewares = append(p.middlewares, middlewares...)
		return nil
	}
}

// WithInsecurePort sets the port that non-encrypted traffic will be served on(optional)
func WithInsecurePort(insecurePort int) Opt {
	return func(p *Proxy) error {
		p.insecurePort = fmt.Sprintf(":%v", insecurePort)
		return nil
	}
}

// WithSecurePort sets the port that encrypted traffic will be served on(optional)
func WithSecurePort(securePort int) Opt {
	return func(p *Proxy) error {
		p.securePort = fmt.Sprintf(":%v", securePort)
		return nil
	}
}

// WithUnaryInterceptors adds gRPC unary interceptors to the proxy instance
func WithUnaryInterceptors(uinterceptors ...grpc.UnaryServerInterceptor) Opt {
	return func(p *Proxy) error {
		p.uinterceptors = append(p.uinterceptors, uinterceptors...)
		return nil
	}
}

// WithStreamInterceptors adds gRPC stream interceptors to the proxy instance
func WithStreamInterceptors(sinterceptors ...grpc.StreamServerInterceptor) Opt {
	return func(p *Proxy) error {
		p.sinterceptors = append(p.sinterceptors, sinterceptors...)
		return nil
	}
}

// WithCertCacheDir sets the directory in which certificates will be cached (default: /tmp/certs)
func WithCertCacheDir(certCache string) Opt {
	return func(p *Proxy) error {
		p.certCache = certCache
		return nil
	}
}

// WithTrigger adds a trigger/expression based route to the reverse proxy
func WithTrigger(triggerExpression string) Opt {
	return func(p *Proxy) error {
		trig, err := trigger.NewArrowTrigger(triggerExpression)
		if err != nil {
			return err
		}
		p.triggers = append(p.triggers, trig)
		return nil
	}
}

// WithAutoRedirectHttps makes the proxy redirect http requests to https(443)
func WithAutoRedirectHttps(redirect bool) Opt {
	return func(p *Proxy) error {
		p.redirectHttps = redirect
		return nil
	}
}

// WithHttpServerOpts executes the functions against the http(s) servers before they start
func WithHttpServerOpts(opts ...func(srv *http.Server)) Opt {
	return func(p *Proxy) error {
		p.httpServerOpts = append(p.httpServerOpts, opts...)
		return nil
	}
}

// WithGrpcServerOpts executes the functions against the grpc servers before they start
func WithGrpcServerOpts(opts ...func(srv *grpc.Server)) Opt {
	return func(p *Proxy) error {
		p.grpcServerOpts = append(p.grpcServerOpts, opts...)
		return nil
	}
}
