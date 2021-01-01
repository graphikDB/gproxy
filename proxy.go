package gproxy

import (
	"context"
	"crypto/tls"
	"github.com/autom8ter/machine"
	"github.com/graphikDB/gproxy/codec"
	"github.com/graphikDB/gproxy/logger"
	grpc_recovery "github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	"github.com/mwitkow/grpc-proxy/proxy"
	"github.com/pkg/errors"
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
	"sync"
	"syscall"
	"time"
)

func init() {
	encoding.RegisterCodec(codec.NewGrpcCodec())
}

// RouterFunc takes a hostname and returns an endpoint to route to
type RouterFunc func(ctx context.Context, host string) string

// Middleware is an http middleware
type Middleware func(handler http.Handler) http.Handler

// Proxy is a secure(lets encrypt) gRPC & http reverse proxy
type Proxy struct {
	mach          *machine.Machine
	logger        *logger.Logger
	gRPCRouter    RouterFunc
	httpRouter    RouterFunc
	middlewares   []Middleware
	uinterceptors []grpc.UnaryServerInterceptor
	sinterceptors []grpc.StreamServerInterceptor
	hostPolicy    autocert.HostPolicy
	certCache     string
	insecurePort  string
	securePort    string
}

// New creates a new proxy instance. A host policy & either http routes, gRPC routes, or both are required.
func New(ctx context.Context, opts ...Opt) (*Proxy, error) {
	p := &Proxy{}
	for _, o := range opts {
		o(p)
	}
	if p.httpRouter == nil && p.gRPCRouter == nil {
		return nil, errors.New("empty http & gRPC router")
	}
	if p.hostPolicy == nil {
		return nil, errors.New("empty host policy")
	}
	if p.insecurePort == "" {
		p.insecurePort = ":80"
	}
	if p.securePort == "" {
		p.securePort = ":443"
	}
	if p.logger == nil {
		p.logger = logger.New(false)
	}
	if p.mach == nil {
		p.mach = machine.New(ctx)
	}

	if p.certCache == "" {
		p.certCache = "/tmp/gproxy"
	}
	return p, nil
}

// Serve starts the gRPC(if grpc router was registered) & http proxy(if http router was registered)
func (p *Proxy) Serve(ctx context.Context) error {
	var (
		m = &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: p.hostPolicy,
			Cache:      autocert.DirCache(p.certCache),
		}
		tlsConfig = &tls.Config{GetCertificate: m.GetCertificate}
		shutdown  []func(ctx context.Context)
	)

	insecure, err := net.Listen("tcp", p.insecurePort)
	if err != nil {
		return err
	}
	secure, err := tls.Listen("tcp", p.securePort, tlsConfig)
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
	var handler http.Handler
	if p.httpDirector() != nil {
		handler = m.HTTPHandler(&httputil.ReverseProxy{
			Director: p.httpDirector(),
		})
	} else {
		handler = m.HTTPHandler(nil)
	}
	//handler = p.panicRecover()(handler)
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
		p.logger.Debug("starting http server", zap.String("address", matcher.Addr().String()))
		if err := httpServer.Serve(matcher); err != nil && err != http.ErrServerClosed {
			p.logger.Error("http proxy failure", zap.Error(err))
		}
	})
	shutdown = append(shutdown, func(ctx context.Context) {
		_ = httpServer.Shutdown(ctx)
	})

	tlsHttpServer := &http.Server{
		Handler: handler,
	}

	p.mach.Go(func(routine machine.Routine) {
		matcher := smux.Match(cmux.Any())
		p.logger.Debug("starting secure http server", zap.String("address", matcher.Addr().String()))
		if err := tlsHttpServer.Serve(matcher); err != nil && err != http.ErrServerClosed {
			p.logger.Error("TLS http proxy failure", zap.Error(err))
		}
	})

	shutdown = append(shutdown, func(ctx context.Context) {
		_ = tlsHttpServer.Shutdown(ctx)
	})
	if p.gRPCRouter != nil {
		gopts := []grpc.ServerOption{
			grpc.ChainUnaryInterceptor(
				grpc_recovery.UnaryServerInterceptor(),
			),
			grpc.ChainStreamInterceptor(
				grpc_recovery.StreamServerInterceptor(),
			),
			grpc.UnknownServiceHandler(proxy.TransparentHandler(p.gRPCDirector())),
		}
		gserver := grpc.NewServer(gopts...)
		p.mach.Go(func(routine machine.Routine) {
			matcher := imux.MatchWithWriters(cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"))
			p.logger.Debug("starting gRPC server", zap.String("address", matcher.Addr().String()))
			if err := gserver.Serve(matcher); err != nil {
				p.logger.Error("gRPC proxy failure", zap.Error(err))
			}
		})
		shutdown = append(shutdown, func(ctx context.Context) {
			stopped := make(chan struct{}, 1)
			go func() {
				gserver.GracefulStop()
				stopped <- struct{}{}
			}()
			select {
			case <-ctx.Done():
				gserver.Stop()
			case <-stopped:
				return
			}
		})
		tlsGserver := grpc.NewServer(gopts...)
		p.mach.Go(func(routine machine.Routine) {
			matcher := smux.MatchWithWriters(cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"))
			p.logger.Debug("starting secure gRPC server", zap.String("address", matcher.Addr().String()))
			if err := tlsGserver.Serve(matcher); err != nil {
				p.logger.Error("TLS gRPC proxy failure", zap.Error(err))
			}
		})
		shutdown = append(shutdown, func(ctx context.Context) {
			stopped := make(chan struct{}, 1)
			go func() {
				tlsGserver.GracefulStop()
				stopped <- struct{}{}
			}()
			select {
			case <-ctx.Done():
				tlsGserver.Stop()
			case <-stopped:
				return
			}
		})
	}

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
	p.logger.Debug("shutdown signal received")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	wg := &sync.WaitGroup{}
	for _, closer := range shutdown {
		wg.Add(1)
		go func(c func(ctx context.Context)) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(shutdownCtx, 5*time.Second)
			defer cancel()
			c(ctx)
		}(closer)
	}
	wg.Wait()
	p.mach.Wait()
	p.logger.Debug("shutdown successful")
	return nil
}

func (p *Proxy) gRPCDirector() proxy.StreamDirector {
	return func(ctx context.Context, fullMethodName string) (context.Context, *grpc.ClientConn, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if ok {
			if val, exists := md[":authority"]; exists && val[0] != "" {
				now := time.Now()
				target := p.gRPCRouter(ctx, val[0])
				fields := []zap.Field{
					zap.String("proxy", "gRPC"),
					zap.Any("metadata", md),
					zap.String("method", fullMethodName),
					zap.String("target", target),
				}
				defer func() {
					dur := time.Since(now)
					fields = append(fields, zap.Duration("duration", dur))
					p.logger.Debug("proxied request", fields...)
				}()
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
		now := time.Now()
		target := p.httpRouter(req.Context(), req.Host)
		fields := []zap.Field{
			zap.String("proxy", "http"),
			zap.String("host", req.Host),
			zap.String("method", req.Method),
			zap.Any("headers", req.Header),
			zap.String("target", target),
		}
		defer func() {
			dur := time.Since(now)
			fields = append(fields, zap.Duration("duration", dur))
			p.logger.Debug("proxied request", fields...)
		}()

		if target == "" {
			p.logger.Debug("empty routing target", fields...)
			return
		} else {
			u, err := url.Parse(target)
			if err != nil {
				fields = append(fields, zap.Error(err))
				p.logger.Debug("failed to parse target", fields...)
				return
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

//
//func getSubdomain(host string) string {
//	host = strings.TrimSpace(host)
//	host_parts := strings.Split(host, ".")
//	if len(host_parts) >= 2 {
//		return host_parts[0]
//	}
//	return ""
//}

func (p *Proxy) panicRecover() Middleware {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					p.logger.Error("panic recover", zap.Error(err.(error)))
				}
			}()
		})
	}
}
