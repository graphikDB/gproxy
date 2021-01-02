package main

import (
	"context"
	"fmt"
	"github.com/graphikDB/gproxy"
	"github.com/graphikDB/gproxy/helpers"
	"github.com/graphikDB/gproxy/logger"
	"github.com/graphikDB/trigger"
	"github.com/pkg/errors"
	"github.com/rs/cors"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"net/url"
)

func init() {
	pflag.CommandLine.StringVar(&configFile, "config", helpers.EnvOr("GPROXY_CONFIG", "gproxy.yaml"), "config file path (env: GPROXY_CONFIG)")
	pflag.Parse()
	viper.SetConfigFile(configFile)
	viper.SetEnvPrefix("GRAPHIKCTL")
	viper.AutomaticEnv()

	viper.SetDefault("server.insecure_port", 80)
	viper.SetDefault("server.secure_port", 443)

	if err := viper.ReadInConfig(); err != nil {
		fmt.Println(err.Error())
		return
	}
}

var (
	configFile string
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var (
		insecurePort = viper.GetInt("server.insecure_port")
		securePort   = viper.GetInt("server.secure_port")
		adomains     = viper.GetStringSlice("autocert")
		corigins     = viper.GetStringSlice("cors.origins")
		cheaders     = viper.GetStringSlice("cors.headers")
		cmethods     = viper.GetStringSlice("cors.methods")
		debug        = viper.GetBool("debug")
		grpcRouter   = viper.GetStringSlice("routing.grpc")
		httpRouter   = viper.GetStringSlice("routing.http")
	)

	lgger := logger.New(debug)
	if len(adomains) == 0 {
		lgger.Error("config: empty autocert",
			zap.Any("config", viper.AllSettings()),
		)
		return
	}
	if len(grpcRouter) == 0 && len(httpRouter) == 0 {
		lgger.Error("config: at least one routing.grpc or routing.http entry expected",
			zap.Any("config", viper.AllSettings()),
		)
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
		var triggers []*trigger.Trigger

		for _, exp := range grpcRouter {
			if exp != "" {
				trig, err := trigger.NewArrowTrigger(exp)
				if err != nil {
					lgger.Error("failed to parse gRPC route trigger", zap.String("trigger", exp), zap.Error(err))
					return
				}
				triggers = append(triggers, trig)
			}
		}

		opts = append(opts, gproxy.WithGRPCRoutes(func(ctx context.Context, host string) string {
			for _, trig := range triggers {
				data, err := trig.Trigger(map[string]interface{}{
					"host": host,
				})
				if err == nil {
					if target, ok := data["target"].(string); ok {
						return target
					}
				}
			}
			return ""
		}))
	}
	if len(httpRouter) > 0 {
		var triggers []*trigger.Trigger

		for _, exp := range httpRouter {
			if exp != "" {
				trig, err := trigger.NewArrowTrigger(exp)
				if err != nil {
					lgger.Error("failed to parse http route trigger", zap.String("trigger", exp), zap.Error(err))
					return
				}
				triggers = append(triggers, trig)
			}
		}
		opts = append(opts, gproxy.WithHTTPRoutes(func(ctx context.Context, host string) string {
			for _, trig := range triggers {
				data, err := trig.Trigger(map[string]interface{}{
					"host": host,
				})
				if err == nil {
					if target, ok := data["target"].(string); ok {
						return target
					}
				}
			}
			return ""
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
