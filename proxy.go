package gproxy

import (
	"context"
	"crypto/tls"
	"github.com/autom8ter/machine"
	"github.com/graphikDB/gproxy/logger"
	grpc_recovery "github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	"github.com/mwitkow/grpc-proxy/proxy"
	"github.com/soheilhy/cmux"
	"go.uber.org/zap"
	"golang.org/x/crypto/acme/autocert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func init() {
	encoding.RegisterCodec(&proxyCodec{codec: proxy.Codec()})
}

type Proxy struct {
	mach        *machine.Machine
	logger      *logger.Logger
	router      func(host string) string
	middlewares []func(handler http.Handler) http.Handler
	hostPolicy  func(ctx context.Context, host string) error
}

func New(ctx context.Context, routes func(host string) string, opts ...Opt) *Proxy {
	p := &Proxy{
		router: routes,
	}
	for _, o := range opts {
		o(p)
	}
	if p.logger == nil {
		p.logger = logger.New(false)
	}
	if p.mach == nil {
		p.mach = machine.New(ctx)
	}
	if p.hostPolicy == nil {
		p.hostPolicy = func(ctx context.Context, host string) error {
			return nil
		}
	}

	return p
}

func (p *Proxy) Serve(ctx context.Context) error {
	m := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: p.hostPolicy,
		Cache:      autocert.DirCache("/tmp/gproxy"),
	}
	tlsConfig := &tls.Config{GetCertificate: m.GetCertificate}

	insecure, err := net.Listen("tcp", ":8080")
	if err != nil {
		return err
	}
	secure, err := tls.Listen("tcp", ":443", tlsConfig)
	if err != nil {
		return err
	}
	defer insecure.Close()
	defer secure.Close()
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupt)

	imux := cmux.New(insecure)
	smux := cmux.New(secure)
	handler := m.HTTPHandler(&httputil.ReverseProxy{
		Director: p.httpDirector(),
	})
	if len(p.middlewares) > 0 {
		for _, h := range p.middlewares {
			handler = h(handler)
		}
	}
	httpServer := &http.Server{
		Handler: handler,
	}

	p.mach.Go(func(routine machine.Routine) {
		matcher := imux.Match(cmux.Any())
		p.logger.Info("starting http server", zap.String("address", matcher.Addr().String()))
		if err := httpServer.Serve(matcher); err != nil && err != http.ErrServerClosed {
			p.logger.Error("http proxy failure", zap.Error(err))
		}
	})
	gopts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(
			//grpc_prometheus.UnaryServerInterceptor,
			//grpc_zap.UnaryServerInterceptor(p.logger.Zap()),
			//grpc_validator.UnaryServerInterceptor(),
			grpc_recovery.UnaryServerInterceptor(),
		),
		grpc.ChainStreamInterceptor(
			//grpc_prometheus.StreamServerInterceptor,
			//grpc_zap.StreamServerInterceptor(p.logger.Zap()),
			//grpc_validator.StreamServerInterceptor(),
			grpc_recovery.StreamServerInterceptor(),
		),
		grpc.UnknownServiceHandler(proxy.TransparentHandler(p.gRPCDirector())),
	}
	gserver := grpc.NewServer(gopts...)
	p.mach.Go(func(routine machine.Routine) {
		matcher := imux.MatchWithWriters(cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"))
		p.logger.Info("starting gRPC server", zap.String("address", matcher.Addr().String()))
		if err := gserver.Serve(matcher); err != nil {
			p.logger.Error("gRPC proxy failure", zap.Error(err))
		}
	})

	tlsHttpServer := &http.Server{
		Handler: handler,
	}

	p.mach.Go(func(routine machine.Routine) {
		matcher := smux.Match(cmux.Any())
		p.logger.Info("starting secure http server", zap.String("address", matcher.Addr().String()))
		if err := tlsHttpServer.Serve(matcher); err != nil && err != http.ErrServerClosed {
			p.logger.Error("TLS http proxy failure", zap.Error(err))
		}
	})
	tlsGserver := grpc.NewServer(gopts...)
	p.mach.Go(func(routine machine.Routine) {
		matcher := smux.MatchWithWriters(cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"))
		p.logger.Info("starting secure gRPC server", zap.String("address", matcher.Addr().String()))
		if err := tlsGserver.Serve(matcher); err != nil {
			p.logger.Error("TLS gRPC proxy failure", zap.Error(err))
		}
	})
	p.mach.Go(func(routine machine.Routine) {
		if err := imux.Serve(); err != nil && !strings.Contains(err.Error(), "closed network connection") {
			p.logger.Error("listener mux error", zap.Error(err))
		}
	})
	p.mach.Go(func(routine machine.Routine) {
		if err := smux.Serve(); err != nil && !strings.Contains(err.Error(), "closed network connection") {
			p.logger.Error("listener mux error", zap.Error(err))
		}
	})
	select {
	case <-interrupt:
		p.mach.Close()
		break
	case <-ctx.Done():
		p.mach.Close()
		break
	}
	p.logger.Warn("shutdown signal received")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	go httpServer.Shutdown(shutdownCtx)
	go tlsHttpServer.Shutdown(shutdownCtx)
	{
		stopped := make(chan struct{})
		go func() {
			gserver.GracefulStop()
			close(stopped)
		}()

		t := time.NewTimer(10 * time.Second)
		select {
		case <-t.C:
			gserver.Stop()
		case <-stopped:
			t.Stop()
		}
	}
	{
		stopped := make(chan struct{})
		go func() {
			tlsGserver.GracefulStop()
			close(stopped)
		}()

		t := time.NewTimer(10 * time.Second)
		select {
		case <-t.C:
			tlsGserver.Stop()
		case <-stopped:
			t.Stop()
		}
	}

	p.mach.Wait()
	p.logger.Debug("shutdown successful")
	return nil
}

func (p *Proxy) gRPCDirector() proxy.StreamDirector {
	return func(ctx context.Context, fullMethodName string) (context.Context, *grpc.ClientConn, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		fields := []zap.Field{
			zap.String("method", fullMethodName),
			zap.Any("metadata", md),
		}
		p.logger.Info("gRPC request received", fields...)
		if ok {
			// Decide on which backend to dial
			if val, exists := md[":authority"]; exists && val[0] != "" {
				if err := p.hostPolicy(ctx, val[0]); err != nil {
					return nil, nil, status.Error(codes.PermissionDenied, err.Error())
				}
				target := p.router(getSubdomain(val[0]))
				if target == "" {
					return nil, nil, status.Error(codes.PermissionDenied, "unknown route")
				}
				// Make sure we use DialContext so the dialing can be cancelled/time out together with the context.
				conn, err := grpc.DialContext(ctx, target, grpc.WithInsecure())
				return ctx, conn, err
			}
		}
		return nil, nil, grpc.Errorf(codes.Unimplemented, "Unknown method")
	}
}

func (p *Proxy) httpDirector() func(r *http.Request) {
	return func(req *http.Request) {
		target := p.router(getSubdomain(req.Host))
		fields := []zap.Field{
			zap.String("scheme", req.URL.Scheme),
			zap.String("method", req.Method),
			zap.String("host", req.Host),
			zap.Any("headers", req.Header),
			zap.Any("target", target),
		}
		p.logger.Info("http request received", fields...)
		if err := p.hostPolicy(req.Context(), req.Host); err != nil {
			p.logger.Error("host policy error", fields...)
			return
		} else {
			if target == "" {
				p.logger.Error("unknown route", fields...)
			} else {
				u, err := url.Parse(target)
				if err != nil {
					panic(err)
				}
				req.URL.Scheme = u.Scheme
				req.URL.Host = u.Host
				req.URL.Path, req.URL.RawPath = joinURLPath(u, req.URL)
				if u.RawQuery == "" || req.URL.RawQuery == "" {
					req.URL.RawQuery = u.RawQuery + req.URL.RawQuery
				} else {
					req.URL.RawQuery = u.RawQuery + "&" + req.URL.RawQuery
				}
				if _, ok := req.Header["User-Agent"]; !ok {
					// explicitly disable User-Agent so it's not set to default value
					req.Header.Set("User-Agent", "")
				}
			}
		}
	}
}

func joinURLPath(a, b *url.URL) (path, rawpath string) {
	if a.RawPath == "" && b.RawPath == "" {
		return singleJoiningSlash(a.Path, b.Path), ""
	}
	// Same as singleJoiningSlash, but uses EscapedPath to determine
	// whether a slash should be added
	apath := a.EscapedPath()
	bpath := b.EscapedPath()

	aslash := strings.HasSuffix(apath, "/")
	bslash := strings.HasPrefix(bpath, "/")

	switch {
	case aslash && bslash:
		return a.Path + b.Path[1:], apath + bpath[1:]
	case !aslash && !bslash:
		return a.Path + "/" + b.Path, apath + "/" + bpath
	}
	return a.Path + b.Path, apath + bpath
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

func getSubdomain(host string) string {
	host = strings.TrimSpace(host)
	host_parts := strings.Split(host, ".")
	if len(host_parts) >= 2 {
		return host_parts[0]
	}
	return ""
}
