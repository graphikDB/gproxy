package gproxy

import (
	"context"
	"github.com/graphikDB/gproxy/logger"
	"net/http"
)

type Opt func(p *Proxy)

func WithHostPolicy(policy func(ctx context.Context, host string) error) Opt {
	return func(p *Proxy) {
		p.hostPolicy = policy
	}
}

func WithLogger(logger *logger.Logger) Opt {
	return func(p *Proxy) {
		p.logger = logger
	}
}

func WithMiddlewares(middlewares ...func(handler http.Handler) http.Handler) Opt {
	return func(p *Proxy) {
		p.middlewares = append(p.middlewares, middlewares...)
	}
}
