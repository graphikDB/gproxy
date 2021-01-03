package main

import (
	"context"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/graphikDB/gproxy"
	"github.com/graphikDB/gproxy/helpers"
	"github.com/graphikDB/gproxy/logger"
	"github.com/rs/cors"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"strings"
)

func init() {
	pflag.CommandLine.StringVar(&configFile, "config", helpers.EnvOr("GPROXY_CONFIG", "gproxy.yaml"), "config file path (env: GPROXY_CONFIG)")
	pflag.Parse()
	viper.SetConfigFile(configFile)
	viper.SetEnvPrefix("GPROXY")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	viper.AutomaticEnv()

	viper.SetDefault("server.insecure_port", 80)
	viper.SetDefault("server.secure_port", 443)

	if err := viper.ReadInConfig(); err != nil {
		if viper.GetBool("debug") {
			fmt.Printf("failed to read in config: %s", err.Error())
		}
		return
	}
	viper.WatchConfig()
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
		policy       = viper.GetString("autocert.policy")
		corigins     = viper.GetStringSlice("cors.origins")
		cheaders     = viper.GetStringSlice("cors.headers")
		cmethods     = viper.GetStringSlice("cors.methods")
		debug        = viper.GetBool("debug")
		routing      = viper.GetStringSlice("routing")
	)

	lgger := logger.New(debug)
	if policy == "" {
		lgger.Debug("config: empty autocert policy")
	}
	if len(routing) == 0 {
		lgger.Error("config: at least one routing trigger/expression entry expected")
		return
	}
	c := cors.New(cors.Options{
		AllowedOrigins: corigins,
		AllowedMethods: cmethods,
		AllowedHeaders: cheaders,
	})
	var opts = []gproxy.Opt{
		gproxy.WithLogger(lgger),
		gproxy.WithHttpInit(gproxy.WithMiddlewares(c.Handler)),
		gproxy.WithHttpsInit(gproxy.WithMiddlewares(c.Handler)),
		gproxy.WithInsecurePort(insecurePort),
		gproxy.WithSecurePort(securePort),
		gproxy.WithAcmePolicy(policy),
	}
	for _, route := range routing {
		opts = append(opts, gproxy.WithTrigger(route))
	}
	proxy, err := gproxy.New(ctx, opts...)
	if err != nil {
		lgger.Error("failed to create proxy", zap.Error(err))
		return
	}
	if viper.GetBool("watch") {
		viper.OnConfigChange(func(in fsnotify.Event) {
			lgger.Debug("config change", zap.String("file", in.Name))
			if err := proxy.OverrideTriggers(viper.GetStringSlice("routing")); err != nil {
				lgger.Error("config change failure", zap.Error(err))
			}
		})
	}
	if err := proxy.Serve(ctx); err != nil {
		lgger.Error("server failure", zap.Error(err))
		return
	}
}
