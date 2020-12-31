package main

import (
	"context"
	"github.com/graphikDB/gproxy"
	"github.com/graphikDB/gproxy/helpers"
	"github.com/graphikDB/gproxy/logger"
	"github.com/graphikDB/gproxy/version"
	"github.com/pkg/errors"
	"github.com/rs/cors"
	"github.com/spf13/pflag"
	"go.uber.org/zap"
)

func init() {
	pflag.CommandLine.BoolVar(&debug, "debug", helpers.BoolEnvOr("GPROXY_DEBUG", false), "enable debug logs (env: GPROXY_DEBUG)")
	pflag.CommandLine.StringToStringVar(&router, "routes", helpers.StringStringEnvOr("GPROXY_ROUTES", nil), "subdomain -> new route transformations (required)")
	pflag.CommandLine.StringSliceVar(&cheaders, "allow-headers", helpers.StringSliceEnvOr("GPROXY_ALLOW_HEADERS", []string{"*"}), "cors allow headers (env: GPROXY_ALLOW_HEADERS)")
	pflag.CommandLine.StringSliceVar(&corigins, "allow-origins", helpers.StringSliceEnvOr("GPROXY_ALLOW_ORIGINS", []string{"*"}), "cors allow origins (env: GPROXY_ALLOW_ORIGINS)")
	pflag.CommandLine.StringSliceVar(&cmethods, "allow-methods", helpers.StringSliceEnvOr("GPROXY_ALLOW_METHODS", []string{"HEAD", "GET", "POST", "PUT", "PATCH", "DELETE"}), "cors allow methods (env: GPROXY_ALLOW_METHODS)")
}

var (
	corigins []string
	cheaders []string
	cmethods []string
	debug    bool
	router   = map[string]string{}
)

func main() {
	pflag.Parse()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lgger := logger.New(debug,
		zap.String("service", "gproxy"),
		zap.String("version", version.Version),
	)
	if router == nil || len(router) == 0 {
		lgger.Error("zero registered routes")
		return
	}
	c := cors.New(cors.Options{
		AllowedOrigins: corigins,
		AllowedMethods: cmethods,
		AllowedHeaders: cheaders,
	})
	var opts = []gproxy.Opt{
		gproxy.WithLogger(lgger),
		gproxy.WithMiddlewares(c.Handler),
	}
	opts = append(opts, gproxy.WithHostPolicy(func(ctx context.Context, host string) error {
		for _, h := range corigins {
			if host == h || h == "*" {
				return nil
			}
		}
		return errors.New("forbidden host")
	}))

	proxy := gproxy.New(ctx, func(host string) string {
		return router[host]
	}, opts...)
	if err := proxy.Serve(ctx); err != nil {
		lgger.Error("server failure", zap.Error(err))
		return
	}
}
