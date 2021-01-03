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

// WithHttpInit executes the functions against the http server before it starts
func WithHttpInit(opts ...func(srv *http.Server)) Opt {
	return func(p *Proxy) error {
		p.httpInit = append(p.httpInit, opts...)
		return nil
	}
}

// WithGrpcInit executes the functions against the insecure grpc server before it starts
func WithGrpcInit(opts ...func(srv *grpc.Server)) Opt {
	return func(p *Proxy) error {
		p.grpcInit = append(p.grpcInit, opts...)
		return nil
	}
}

// WithHttpsInit executes the functions against the https server before it starts
func WithHttpsInit(opts ...func(srv *http.Server)) Opt {
	return func(p *Proxy) error {
		p.httpsInit = append(p.httpInit, opts...)
		return nil
	}
}

// WithGrpcsInit executes the functions against the grpc secure server before it starts
func WithGrpcsInit(opts ...func(srv *grpc.Server)) Opt {
	return func(p *Proxy) error {
		p.grpcsInit = append(p.grpcInit, opts...)
		return nil
	}
}
