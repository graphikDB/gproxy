package main

import (
	"context"
	"github.com/graphikDB/gproxy"
	"github.com/graphikDB/gproxy/helpers"
	"github.com/graphikDB/gproxy/logger"
	"github.com/pkg/errors"
	"github.com/rs/cors"
	"github.com/spf13/pflag"
	"go.uber.org/zap"
	"net/url"
)

func init() {
	pflag.CommandLine.BoolVar(&debug, "debug", helpers.BoolEnvOr("GPROXY_DEBUG", false), "enable debug logs (env: GPROXY_DEBUG)")
	pflag.CommandLine.IntVar(&insecurePort, "insecure-port", helpers.IntEnvOr("GPROXY_INSECURE_PORT", 80), "insecure port to serve on (env: GPROXY_INSECURE_PORT)")
	pflag.CommandLine.IntVar(&securePort, "secure-port", helpers.IntEnvOr("GPROXY_SECURE_PORT", 443), "secure port to serve on (env: GPROXY_SECURE_PORT)")

	pflag.CommandLine.StringToStringVar(&grpcRouter, "grpc-routes", helpers.StringStringEnvOr("GPROXY_GRPC_ROUTES", nil), "hostname -> gRPC endpoint/url transformations")
	pflag.CommandLine.StringToStringVar(&httpRouter, "http-routes", helpers.StringStringEnvOr("GPROXY_HTTP_ROUTES", nil), "hostname -> http endpoint/url transformations")
	pflag.CommandLine.StringSliceVar(&adomains, "allow-domains", helpers.StringSliceEnvOr("GPROXY_ALLOW_DOMAINS", nil), "allowed domains for lets encrypt autocert (required) (env: GPROXY_ALLOW_DOMAINS)")
	pflag.CommandLine.StringSliceVar(&cheaders, "allow-headers", helpers.StringSliceEnvOr("GPROXY_ALLOW_HEADERS", []string{"*"}), "cors allow headers (env: GPROXY_ALLOW_HEADERS)")
	pflag.CommandLine.StringSliceVar(&corigins, "allow-origins", helpers.StringSliceEnvOr("GPROXY_ALLOW_ORIGINS", []string{"*"}), "cors allow origins (env: GPROXY_ALLOW_ORIGINS)")
	pflag.CommandLine.StringSliceVar(&cmethods, "allow-methods", helpers.StringSliceEnvOr("GPROXY_ALLOW_METHODS", []string{"HEAD", "GET", "POST", "PUT", "PATCH", "DELETE"}), "cors allow methods (env: GPROXY_ALLOW_METHODS)")
}

var (
	insecurePort int
	securePort   int
	adomains     []string
	corigins     []string
	cheaders     []string
	cmethods     []string
	debug        bool
	grpcRouter   = map[string]string{}
	httpRouter   = map[string]string{}
)

func main() {
	pflag.Parse()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lgger := logger.New(debug)
	if len(adomains) == 0 {
		lgger.Error("empty --allow-domains")
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
		gproxy.WithInsecurePort(insecurePort),
		gproxy.WithSecurePort(securePort),
	}
	opts = append(opts, gproxy.WithHostPolicy(func(ctx context.Context, host string) error {
		for _, h := range adomains {
			if host == h {
				return nil
			}
		}
		return errors.New("forbidden domain")
	}))
	if len(grpcRouter) > 0 {
		for _, v := range grpcRouter {
			if _, err := url.Parse(v); err != nil {
				lgger.Error("failed to parse gRPC route", zap.String("route", v), zap.Error(err))
				return
			}
		}
		opts = append(opts, gproxy.WithGRPCRoutes(func(host string) string {
			return grpcRouter[host]
		}))
	}
	if len(httpRouter) > 0 {
		for _, v := range httpRouter {
			if _, err := url.Parse(v); err != nil {
				lgger.Error("failed to parse http route", zap.String("route", v), zap.Error(err))
				return
			}
		}
		opts = append(opts, gproxy.WithHTTPRoutes(func(host string) string {
			return httpRouter[host]
		}))
	}

	proxy, err := gproxy.New(ctx, opts...)
	if err != nil {
		lgger.Error("failed to create proxy", zap.Error(err))
		return
	}
	if err := proxy.Serve(ctx); err != nil {
		lgger.Error("server failure", zap.Error(err))
		return
	}
}
